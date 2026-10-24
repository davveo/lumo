// Package devstack starts an isolated development cluster. Each service owns a
// separate SQLite file and a real TCP gRPC server. This is deliberately not the
// Docker/MySQL/RabbitMQ production runtime: a durable local outbox pump replaces
// the broker so frontend integration can run without a Docker daemon.
package devstack

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/davveo/lumo/internal/bootstrap"
	"github.com/davveo/lumo/internal/platform"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Stack struct {
	RPC       platform.Caller
	modules   map[string]platform.Module
	dbs       map[string]*gorm.DB
	servers   []*grpc.Server
	listeners []net.Listener
	mu        sync.Mutex
}

// Start must be called with DEMO_MODE=true only for local/test use. The caller
// owns the HTTP server and calls Pump/Tick periodically (or deterministically in
// tests). An existing data directory preserves orders and sessions across runs.
func Start(ctx context.Context, dataDir, token string) (*Stack, error) {
	if !sqliteRuntimeAvailable {
		return nil, fmt.Errorf(
			"devstack SQLite requires CGO_ENABLED=1 and a C compiler; use Docker Compose for the normal MySQL runtime",
		)
	}
	if os.Getenv("DEMO_MODE") != "true" {
		return nil, fmt.Errorf("devstack requires explicit DEMO_MODE=true")
	}
	if token == "" {
		return nil, fmt.Errorf("internal development token required")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	s := &Stack{modules: map[string]platform.Module{}, dbs: map[string]*gorm.DB{}}
	targets := map[string]string{}
	for _, name := range bootstrap.Names {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			s.Close()
			return nil, err
		}
		s.listeners = append(s.listeners, l)
		targets[name] = l.Addr().String()
	}
	s.RPC = platform.NewCaller(targets, token)
	for i, name := range bootstrap.Names {
		db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(dataDir, name+".db"))
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("%s database: %w", name, err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			s.Close()
			return nil, err
		}
		// Single connection per service avoids SQLite write-lock contention; different
		// services still execute concurrently through independent database files.
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		s.dbs[name] = db
		if err = platform.MigratePlatform(ctx, db); err != nil {
			s.Close()
			return nil, err
		}
		m, err := bootstrap.New(name, platform.Dependencies{DB: db, RPC: s.RPC})
		if err != nil {
			s.Close()
			return nil, err
		}
		if m.Migrate != nil {
			if err = m.Migrate(ctx); err != nil {
				s.Close()
				return nil, fmt.Errorf("%s migrate: %w", name, err)
			}
		}
		if m.Seed != nil {
			if err = m.Seed(ctx); err != nil {
				s.Close()
				return nil, fmt.Errorf("%s seed: %w", name, err)
			}
		}
		server, err := platform.NewRPCServer(token, m)
		if err != nil {
			s.Close()
			return nil, err
		}
		s.modules[name] = m
		s.servers = append(s.servers, server)
		go func(server *grpc.Server, l net.Listener) { _ = server.Serve(l) }(server, s.listeners[i])
	}
	return s, nil
}

func (s *Stack) Close() {
	for _, server := range s.servers {
		server.Stop()
	}
	for _, l := range s.listeners {
		_ = l.Close()
	}
	if closer, ok := s.RPC.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	for _, db := range s.dbs {
		if d, e := db.DB(); e == nil {
			_ = d.Close()
		}
	}
}

func (s *Stack) Tick(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range bootstrap.Names {
		m := s.modules[name]
		if m.Tick != nil {
			if err := m.Tick(ctx); err != nil {
				return fmt.Errorf("%s scheduled task: %w", name, err)
			}
		}
	}
	return nil
}

// Pump delivers committed outbox rows with their original W3C parent. A durable
// per-consumer inbox skips successful deliveries on retries/restart. It provides
// at-least-once delivery, and service handlers retain their own idempotency.
func (s *Stack) Pump(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for round := 0; round < 80; round++ {
		count := 0
		for _, producer := range bootstrap.Names {
			var rows []platform.Outbox
			if err := s.dbs[producer].WithContext(ctx).Where(
				"state = ? AND next_attempt_at <= ?",
				constants.OutboxStatePending,
				time.Now().UTC(),
			).Order(
				"created_at ASC",
			).Limit(
				100,
			).Find(
				&rows,
			).Error; err != nil {
				return err
			}
			for _, row := range rows {
				count++
				var event platform.Event
				if err := json.Unmarshal([]byte(row.PayloadJSON), &event); err != nil {
					return err
				}
				carrier := propagation.MapCarrier{}
				if err := json.Unmarshal([]byte(row.CarrierJSON), &carrier); err != nil {
					return err
				}
				for _, consumer := range bootstrap.Names {
					m := s.modules[consumer]
					if m.OnEvent == nil {
						continue
					}
					match := false
					for _, topic := range m.Topics {
						if topic == event.Type || topic == "#" {
							match = true
						}
					}
					if !match {
						continue
					}
					db := s.dbs[consumer]
					var seen int64
					if err := db.WithContext(ctx).Model(&platform.Inbox{}).Where(
						"consumer = ? AND event_id = ? AND state = ?",
						consumer,
						event.ID,
						constants.InboxStateDone,
					).Count(
						&seen,
					).Error; err != nil {
						return err
					}
					if seen > 0 {
						continue
					}
					eventCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)
					eventCtx, sp := otel.Tracer("lumo/devstack").
						Start(
							eventCtx,
							"localmq.consume."+event.Type,
							trace.WithSpanKind(trace.SpanKindConsumer),
							trace.WithAttributes(
								attribute.String("messaging.consumer", consumer),
								attribute.String("messaging.system", "local-durable-outbox"),
							),
						)
					platform.TraceRequest(eventCtx, event)
					err := m.OnEvent(eventCtx, event)
					if err != nil {
						platform.TraceResponse(eventCtx, nil, err)
						sp.End()
						_ = s.dbs[producer].WithContext(ctx).
							Model(&platform.Outbox{}).
							Where("id = ?", row.ID).
							Updates(
								map[string]any{
									"attempts":        gorm.Expr("attempts + 1"),
									"next_attempt_at": time.Now().UTC().Add(time.Second),
								},
							).
							Error
						return fmt.Errorf("%s -> %s (%s): %w", producer, consumer, event.Type, err)
					}
					inbox := platform.Inbox{
						Consumer:  consumer,
						EventID:   event.ID,
						State:     constants.InboxStateDone,
						Attempts:  1,
						UpdatedAt: time.Now().UTC(),
					}
					err = db.WithContext(eventCtx).Clauses(clause.OnConflict{DoNothing: true}).Create(&inbox).Error
					platform.TraceResponse(
						eventCtx,
						platform.Result{"event_id": event.ID, "inbox_recorded": err == nil},
						err,
					)
					sp.End()
					if err != nil {
						return err
					}
				}
				now := time.Now().UTC()
				if err := s.dbs[producer].WithContext(ctx).Model(&platform.Outbox{}).Where("id = ?", row.ID).Updates(
					map[string]any{"state": constants.OutboxStatePublished, "published_at": now},
				).Error; err != nil {
					return err
				}
			}
		}
		if count == 0 {
			return nil
		}
	}
	return fmt.Errorf("local outbox did not converge within 80 passes")
}
