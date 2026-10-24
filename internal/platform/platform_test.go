package platform

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func tracing(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	old, prop := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(old)
		otel.SetTextMapPropagator(prop)
	})
	return exporter, provider
}
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDB(context.Background(), "sqlite", t.TempDir()+"/platform.db")
	if err != nil {
		t.Fatal(err)
	}
	if err = MigratePlatform(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw, _ := db.DB(); _ = raw.Close() })
	return db
}

func TestRPCTraceReachesEveryLayerAndSQL(t *testing.T) {
	exporter, provider := tracing(t)
	db := testDB(t)
	type privateRecord struct {
		ID     string `gorm:"primaryKey"`
		Secret string
	}
	if err := db.AutoMigrate(&privateRecord{}); err != nil {
		t.Fatal(err)
	}
	exporter.Reset()
	module := Module{Name: "catalog", Handle: func(ctx context.Context, c Command) (Result, error) {
		ctx, usecase := Start(ctx, "catalog.usecase.Create")
		defer usecase.End()
		ctx, repository := Start(ctx, "catalog.repository.Save")
		defer repository.End()
		return Result{
			"id": c.ID,
		}, db.WithContext(ctx).
			Create(&privateRecord{ID: c.ID, Secret: "do-not-export-password"}).
			Error
	}}
	server, err := NewRPCServer("test-internal-credential", module)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	caller := NewCaller(map[string]string{"catalog": listener.Addr().String()}, "test-internal-credential")
	t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
	ctx, root := Start(context.Background(), "http.test.create")
	result, err := caller.Call(ctx, "catalog", Command{Action: "create", ID: "bound-private-id", UserID: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	root.End()
	if result["id"] != "bound-private-id" {
		t.Fatal(result)
	}
	if err = provider.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	found := map[string]bool{}
	var repositoryID string
	for _, s := range spans {
		if s.SpanContext.TraceID() != root.SpanContext().TraceID() {
			t.Fatalf("split trace at %s", s.Name)
		}
		found[s.Name] = true
		if s.Name == "catalog.repository.Save" {
			repositoryID = s.SpanContext.SpanID().String()
		}
	}
	for _, name := range []string{
		"http.test.create",
		"rpc.client.catalog.create",
		"transport.grpc.catalog.create",
		"catalog.usecase.Create",
		"catalog.repository.Save",
		"SQL INSERT private_records",
	} {
		if !found[name] {
			t.Errorf("missing %s; spans=%v", name, found)
		}
	}
	for _, s := range spans {
		attrs := rpcSpanAttributes(s)
		if s.Name == "rpc.client.catalog.create" || s.Name == "transport.grpc.catalog.create" {
			if attrs["lumo.target_service"] != "catalog" || attrs["lumo.action"] != "create" {
				t.Errorf("business target missing from %s: %v", s.Name, attrs)
			}
		}
		if s.Name == "mall.v1.DomainService/Execute" {
			if attrs["lumo.target_service"] != "catalog" || attrs["rpc.service"] != "mall.v1.DomainService" {
				t.Errorf("gRPC business target or protobuf service wrong: %v", attrs)
			}
			if s.SpanKind == trace.SpanKindClient && attrs["peer.service"] != "catalog" {
				t.Errorf("client peer service missing: %v", attrs)
			}
			if s.SpanKind == trace.SpanKindServer && attrs["lumo.action"] != "create" {
				t.Errorf("server action missing: %v", attrs)
			}
		}
		if strings.HasPrefix(s.Name, "SQL INSERT") {
			if s.Parent.SpanID().String() != repositoryID {
				t.Errorf("SQL parent is not repository")
			}
			for _, a := range s.Attributes {
				if strings.Contains(a.Value.AsString(), "do-not-export-password") ||
					strings.Contains(a.Value.AsString(), "bound-private-id") {
					t.Errorf("query variables leaked in %s", a.Key)
				}
			}
		}
	}
}

func TestRPCAuthenticationAndPublicHealth(t *testing.T) {
	exporter, _ := tracing(t)
	server, err := NewRPCServer(
		"correct-token",
		Module{Name: "test", Handle: func(context.Context, Command) (Result, error) { return Result{"ok": true}, nil }},
	)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	caller := NewCaller(map[string]string{"test": listener.Addr().String()}, "wrong-token")
	t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
	if _, err = caller.Call(context.Background(), "test", Command{Action: "read"}); status.Code(
		err,
	) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated, got %v", err)
	}
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if result, err := healthv1.NewHealthClient(conn).Check(ctx, &healthv1.HealthCheckRequest{}); err != nil ||
		result.Status != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("health failed: %v %v", result, err)
	}
	for _, span := range awaitRPCTraceSpans(t, exporter, 4) {
		attrs := rpcSpanAttributes(span)
		if span.SpanKind != trace.SpanKindServer {
			continue
		}
		switch span.Name {
		case "mall.v1.DomainService/Execute":
			if attrs["lumo.target_service"] != "test" || attrs["lumo.action"] != "" {
				t.Errorf("rejected RPC must identify target without accepting action: %v", attrs)
			}
		case "grpc.health.v1.Health/Check":
			if attrs["lumo.target_service"] != "" || attrs["lumo.action"] != "" {
				t.Errorf("health check mislabeled as business command: %v", attrs)
			}
		}
	}
}

func TestOutboxTransactionAndTraceCarrier(t *testing.T) {
	tracing(t)
	db := testDB(t)
	ctx, span := Start(context.Background(), "usecase.place")
	defer span.End()
	event := Event{
		ID:          "event-1",
		Type:        "OrderAwaitingPayment",
		AggregateID: "order-1",
		UserID:      "demo",
		Data:        map[string]any{"order_id": "order-1"},
	}
	rollback := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := Enqueue(ctx, tx, event); err != nil {
			return err
		}
		return status.Error(codes.Aborted, "rollback")
	})
	if rollback == nil {
		t.Fatal("expected rollback")
	}
	var count int64
	if err := db.Model(&Outbox{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("outbox escaped business transaction", count, err)
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return Enqueue(ctx, tx, event) }); err != nil {
		t.Fatal(err)
	}
	if err := Enqueue(ctx, db, event); err != nil {
		t.Fatal(err)
	}
	var rows []Outbox
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatal("event id not idempotent")
	}
	var carrier propagation.MapCarrier
	if err := json.Unmarshal([]byte(rows[0].CarrierJSON), &carrier); err != nil {
		t.Fatal(err)
	}
	restored := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
	if got := otel.GetTracerProvider().Tracer("test"); got == nil {
		t.Fatal("no tracer")
	}
	_, child := Start(restored, "mq.restored")
	defer child.End()
	if child.SpanContext().TraceID() != span.SpanContext().TraceID() {
		t.Fatal("outbox trace context lost")
	}
}

func TestInboxLeaseAndReplay(t *testing.T) {
	tracing(t)
	db := testDB(t)
	ctx := context.Background()
	first, owned, err := claimInbox(ctx, db, "orders", "event-1")
	if err != nil || !owned {
		t.Fatal(first, owned, err)
	}
	_, owned, err = claimInbox(ctx, db, "orders", "event-1")
	if err != nil || owned {
		t.Fatal("duplicate concurrently claimed", owned, err)
	}
	if err = markInbox(ctx, db, first, "RETRY"); err != nil {
		t.Fatal(err)
	}
	second, owned, err := claimInbox(ctx, db, "orders", "event-1")
	if err != nil || !owned || second.Attempts != 2 {
		t.Fatal(second, owned, err)
	}
	if err = markInbox(ctx, db, second, "DONE"); err != nil {
		t.Fatal(err)
	}
	old, owned, err := claimInbox(ctx, db, "orders", "event-1")
	if err != nil || owned || old.State != "DONE" {
		t.Fatal(old, owned, err)
	}
}

func TestSQLTemplateDoesNotLeakLiterals(t *testing.T) {
	got := SQLTemplate(
		"SELECT * FROM users WHERE password = 'super-secret' AND id = 123 AND email = \"private@example.test\"",
	)
	for _, private := range []string{"super-secret", "123", "private@example.test"} {
		if strings.Contains(got, private) {
			t.Fatalf("leaked %q: %s", private, got)
		}
	}
}
