package main

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/davveo/lumo/internal/bootstrap"
	"github.com/davveo/lumo/internal/platform"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"gorm.io/gorm"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	name := env("SERVICE_NAME", constants.ServiceCatalog)
	shutdown, err := platform.InitTelemetry(ctx, name, os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := shutdown(flush); e != nil {
			slog.Warn("trace flush failed", "error", e)
		}
	}()
	driver := env("DB_DRIVER", "mysql")
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return fmt.Errorf("DB_DSN is required; use Docker Compose or set an isolated sqlite path for local development")
	}
	var db *gorm.DB
	deadline := time.Now().Add(60 * time.Second)
	for {
		db, err = platform.OpenDB(ctx, driver, dsn)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("database unavailable: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(15 * time.Minute)
	var cache *redis.Client
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		cache = redis.NewClient(
			&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD"), DialTimeout: 3 * time.Second},
		)
		defer cache.Close()
	}
	consul, err := platform.ConsulFromEnv()
	if err != nil {
		return fmt.Errorf("Consul configuration: %w", err)
	}
	targets := map[string]string{}
	for _, n := range bootstrap.Names {
		fallback := n + ":9000"
		if consul != nil {
			fallback = "consul:///" + n
		}
		targets[n] = env(strings.ToUpper(n)+"_RPC_ADDR", fallback)
	}
	rpc := platform.NewCallerWithConsul(targets, os.Getenv("INTERNAL_RPC_TOKEN"), consul)
	defer func() {
		if closer, ok := rpc.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()
	module, err := bootstrap.New(name, platform.Dependencies{DB: db, RPC: rpc, Redis: cache})
	if err != nil {
		return err
	}
	if err = platform.WithServiceStartupLock(ctx, db, name, func(startupCtx context.Context) error {
		if err := platform.MigratePlatform(startupCtx, db); err != nil {
			return fmt.Errorf("platform migration: %w", err)
		}
		if module.Migrate != nil {
			if err := module.Migrate(startupCtx); err != nil {
				return fmt.Errorf("%s migration: %w", name, err)
			}
		}
		if env("DEMO_MODE", "false") == "true" && module.Seed != nil {
			if err := module.Seed(startupCtx); err != nil {
				return fmt.Errorf("%s demo seed: %w", name, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("%s startup initialization: %w", name, err)
	}
	unregister, err := platform.RegisterConsulService(ctx, consul, name, env("RPC_ADDR", ":9000"), "grpc")
	if err != nil {
		return fmt.Errorf("Consul registration: %w", err)
	}
	defer unregister()
	if os.Getenv("AMQP_URL") == "" {
		slog.Warn("AMQP_URL empty: durable outbox is retained; no external message transport running")
	}
	// RunEvents also coordinates scheduled work when AMQP is disabled; all
	// replicas share the same cross-process task exclusion in either mode.
	failures, finishWorkers := startWorkers(ctx,
		func(workerCtx context.Context) error {
			return platform.RunEvents(workerCtx, db, cache, os.Getenv("AMQP_URL"), module)
		},
		func(workerCtx context.Context) error {
			return platform.ServeRPC(workerCtx, env("RPC_ADDR", ":9000"), os.Getenv("INTERNAL_RPC_TOKEN"), module)
		},
	)
	defer finishWorkers()
	slog.Info(
		"service started",
		"service",
		name,
		"rpc_addr",
		env("RPC_ADDR", ":9000"),
		"database_driver",
		driver,
		"demo_mode",
		env("DEMO_MODE", "false"),
	)
	select {
	case <-ctx.Done():
		return nil
	case e := <-failures:
		stop()
		return e
	}
}

func healthcheck() error {
	addr := env("RPC_ADDR", ":9000")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer cc.Close()
	r, err := healthpb.NewHealthClient(cc).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if r.Status != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("not serving")
	}
	return nil
}
