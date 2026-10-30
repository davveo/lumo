package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	amqp "github.com/rabbitmq/amqp091-go"
	otelcodes "go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTraceErrorPreservesSpecificReasonAndCauseWithoutRequestSecrets(t *testing.T) {
	err := fmt.Errorf("reserve SKU sku-demo requested=3 available=1: %w", status.Error(codes.FailedPrecondition, "stock insufficient; buyer-user-private; account-secret-private"))
	span, attrs := captureTracePayload(t, map[string]any{"user_id": "buyer-user-private", "password": "account-secret-private"}, nil, err)
	if !strings.Contains(attrs["error.message"].AsString(), "requested=3 available=1") || !strings.Contains(attrs["lumo.error.causes"].AsString(), "stock insufficient") || len(span.Events) == 0 {
		t.Fatal("specific diagnostics missing", attrs)
	}
	encoded, _ := json.Marshal(span)
	for _, secret := range []string{"buyer-user-private", "account-secret-private"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("secret escaped redaction", secret)
		}
	}
	if span.Events[0].Name != "exception" || !attrs["lumo.error.redacted"].AsBool() || span.Status.Code != otelcodes.Error {
		t.Fatal(span)
	}
}

func TestTraceErrorRetainsOriginalFailureBeforeRPCConversionAndSuccessfulAck(t *testing.T) {
	exporter, _ := tracing(t)
	ctx, span := Start(context.Background(), "test.repository.failure")
	TraceError(ctx, fmt.Errorf("insert order failed: %w", errors.New("UNIQUE constraint failed: orders.id")))
	TraceResponse(ctx, nil, status.Error(codes.AlreadyExists, "operation failed"))
	TraceResponse(ctx, Result{"acknowledged": true}, nil)
	span.End()
	got := exporter.GetSpans()[0]
	attrs := rpcSpanAttributes(got)
	if !strings.Contains(attrs["error.message"], "UNIQUE constraint failed: orders.id") || attrs["lumo.error.code"] != "AlreadyExists" || got.Status.Code != otelcodes.Error {
		t.Fatal("original failure was overwritten", got)
	}
}

func TestTraceErrorRedactsConnectionCredentialsAndBoundsDiagnosticSize(t *testing.T) {
	ctx := NewTraceErrorContext(context.Background())
	raw := "database connection failed account:raw-password@tcp(database:3306); amqp://mall:broker-password@rabbitmq:5672/; https://provider.test/callback?token=callback-secret; phone=13912345678; " + strings.Repeat("错误", 10000)
	clean, redacted, truncated := safeTraceErrorText(ctx, raw)
	if len(clean) > 2051 || !redacted || !truncated || !strings.Contains(clean, "database connection failed") {
		t.Fatal(len(clean), redacted, truncated)
	}
	for _, secret := range []string{"raw-password", "broker-password", "callback-secret", "13912345678"} {
		if strings.Contains(clean, secret) {
			t.Fatal("credential leaked", secret)
		}
	}
	// A fresh logical request cannot inherit another request's remembered values.
	errorContext(ctx).remember("normal-diagnostic-word")
	next := NewTraceErrorContext(ctx)
	clean, _, _ = safeTraceErrorText(next, "normal-diagnostic-word")
	if clean != "normal-diagnostic-word" {
		t.Fatal("redaction state leaked across requests")
	}
}

func TestSQLSpanIncludesRealDatabaseFailureWithParameterizedQuery(t *testing.T) {
	exporter, _ := tracing(t)
	ctx, root := Start(context.Background(), "test.repository.missing_table")
	db, err := OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	err = db.WithContext(ctx).Exec("INSERT INTO missing_trace_table (value) VALUES (?)", "private-bound-value").Error
	if err == nil {
		t.Fatal("expected real SQLite failure")
	}
	root.End()
	found := false
	for _, span := range exporter.GetSpans() {
		attrs := rpcSpanAttributes(span)
		if attrs["code.layer"] != "database" || !strings.Contains(attrs["db.query.text"], "missing_trace_table") {
			continue
		}
		found = true
		if !strings.Contains(attrs["error.message"], "no such table: missing_trace_table") || len(span.Events) == 0 {
			t.Fatal("database reason missing", span)
		}
		encoded, _ := json.Marshal(span)
		if strings.Contains(string(encoded), "private-bound-value") {
			t.Fatal("bound SQL value leaked")
		}
	}
	if !found {
		t.Fatal("SQL error span missing")
	}
}

func TestConsumerTraceKeepsBusinessFailureWhenDeadLetterAckSucceeds(t *testing.T) {
	exporter, _ := tracing(t)
	ctx := context.Background()
	db, err := OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "consumer.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err = MigratePlatform(ctx, db); err != nil {
		t.Fatal(err)
	}
	event := Event{ID: "event-trace-failed", Type: constants.EventOrderCompleted, AggregateID: "order-demo", UserID: "buyer-private"}
	raw, _ := json.Marshal(event)
	in := Inbox{Consumer: "notification", EventID: event.ID, State: constants.InboxStateRetry, Attempts: 4, LeaseUntil: time.Now().Add(-time.Minute)}
	if err = db.WithContext(ctx).Create(&in).Error; err != nil {
		t.Fatal(err)
	}
	ack := &traceDeliveryAcknowledgement{}
	m := Module{Name: "notification", Topics: []string{event.Type}, OnEvent: func(context.Context, Event) error {
		return status.Error(codes.FailedPrecondition, "notification template missing for OrderCompleted")
	}}
	if err = consumeOne(ctx, db, nil, m, amqp.Delivery{Body: raw, Acknowledger: ack}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, span := range exporter.GetSpans() {
		if span.Name != "mq.consume."+event.Type {
			continue
		}
		found = true
		attrs := rpcSpanAttributes(span)
		if span.Status.Code != otelcodes.Error || attrs["lumo.error.code"] != "FailedPrecondition" || !strings.Contains(attrs["error.message"], "notification template missing") || !strings.Contains(attrs["lumo.response"], "dead_letter") {
			t.Fatal("ack hid consumer failure", span)
		}
	}
	if !found {
		t.Fatal("consumer trace missing")
	}
}

func TestRPCTraceKeepsUnderlyingReasonBeforeGenericTransportError(t *testing.T) {
	exporter, _ := tracing(t)
	address := rpcTraceServer(t, Module{Name: "catalog", Handle: func(context.Context, Command) (Result, error) {
		return nil, fmt.Errorf("warehouse reservation rejected: %w", errors.New("quota exhausted for sku-visible"))
	}})
	caller := NewCaller(map[string]string{"catalog": address}, "trace-internal-token")
	t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
	_, err := caller.Call(context.Background(), "catalog", Command{Action: "get", ID: "sku-visible"})
	if status.Code(err) != codes.Internal || status.Convert(err).Message() != "operation failed" {
		t.Fatal("caller response changed", err)
	}
	found := false
	for _, span := range awaitRPCTraceSpans(t, exporter, 4) {
		if span.Name != "transport.grpc.catalog.get" {
			continue
		}
		found = true
		attrs := rpcSpanAttributes(span)
		if !strings.Contains(attrs["error.message"], "quota exhausted for sku-visible") || !strings.Contains(attrs["lumo.error.causes"], "warehouse reservation rejected") {
			t.Fatal("transport discarded original failure", span)
		}
	}
	if !found {
		t.Fatal("RPC origin span missing")
	}
}

func TestInvalidMessageProducesTraceBeforeRejection(t *testing.T) {
	exporter, _ := tracing(t)
	called := false
	m := Module{Name: "notification", OnEvent: func(context.Context, Event) error { called = true; return nil }}
	ack := &traceDeliveryAcknowledgement{}
	if err := consumeOne(context.Background(), nil, nil, m, amqp.Delivery{Body: []byte("{"), Acknowledger: ack}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("malformed message reached business code")
	}
	found := false
	for _, span := range exporter.GetSpans() {
		if span.Name != "mq.consume.rejected" {
			continue
		}
		found = true
		attrs := rpcSpanAttributes(span)
		if attrs["lumo.error.code"] != "InvalidArgument" || !strings.Contains(attrs["error.message"], "unexpected EOF") || len(span.Events) == 0 {
			t.Fatal(span)
		}
	}
	if !found {
		t.Fatal("rejected message has no diagnostic trace")
	}
}

func TestOversizedSensitiveValueCannotLeakAfterErrorTruncation(t *testing.T) {
	secret := strings.Repeat("private-password-material-", 800)
	span, attrs := captureTracePayload(t, map[string]any{"password": secret}, nil, fmt.Errorf("provider verification failed: %s", secret))
	encoded, _ := json.Marshal(span)
	if strings.Contains(string(encoded), "private-password-material") || !strings.Contains(attrs["error.message"].AsString(), "provider verification failed") || !attrs["lumo.error.truncated"].AsBool() {
		t.Fatal("truncation exposed a sensitive prefix", attrs)
	}
}
