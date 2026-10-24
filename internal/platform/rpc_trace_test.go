package platform

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func rpcSpanAttributes(span tracetest.SpanStub) map[string]string {
	attrs := make(map[string]string, len(span.Attributes))
	for _, attr := range span.Attributes {
		attrs[string(attr.Key)] = attr.Value.AsString()
	}
	return attrs
}

func rpcTraceServer(t *testing.T, module Module) string {
	t.Helper()
	server, err := NewRPCServer("trace-internal-token", module)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	return listener.Addr().String()
}

func awaitRPCTraceSpans(t *testing.T, exporter *tracetest.InMemoryExporter, count int) tracetest.SpanStubs {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		spans := exporter.GetSpans()
		if len(spans) >= count {
			return spans
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d spans, want at least %d", len(spans), count)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRPCTraceIdentifiesEachTargetInFanout(t *testing.T) {
	exporter, _ := tracing(t)
	pricing := rpcTraceServer(t, Module{Name: "pricing", Handle: func(context.Context, Command) (Result, error) {
		return Result{"ok": true}, nil
	}})
	var caller Caller
	checkout := rpcTraceServer(
		t,
		Module{Name: "checkout", Handle: func(ctx context.Context, command Command) (Result, error) {
			return caller.Call(ctx, "pricing", Command{Action: "quote"})
		}},
	)
	caller = NewCaller(map[string]string{"checkout": checkout, "pricing": pricing}, "trace-internal-token")
	t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
	ctx, root := Start(context.Background(), "http.test.submit")
	_, err := caller.Call(
		ctx,
		"checkout",
		Command{Action: "submit", Data: map[string]any{"secret": "do-not-export-command-body"}},
	)
	root.End()
	if err != nil {
		t.Fatal(err)
	}
	spans := awaitRPCTraceSpans(t, exporter, 9)
	clients, servers, business := 0, 0, 0
	for _, span := range spans {
		if span.SpanContext.TraceID() != root.SpanContext().TraceID() {
			t.Fatalf("trace split at %s", span.Name)
		}
		attrs := rpcSpanAttributes(span)
		for _, value := range attrs {
			if strings.Contains(value, "trace-internal-token") ||
				strings.Contains(value, "do-not-export-command-body") {
				t.Errorf("request credential/body leaked in %s", span.Name)
			}
		}
		if span.Name == "http.test.submit" {
			continue
		}
		target := attrs["lumo.target_service"]
		action := map[string]string{"checkout": "submit", "pricing": "quote"}[target]
		if action == "" {
			t.Errorf("missing per-hop target on %s: %v", span.Name, attrs)
		}
		if span.Name == "mall.v1.DomainService/Execute" {
			if attrs["rpc.service"] != "mall.v1.DomainService" {
				t.Errorf("protobuf service overwritten on %s: %v", span.Name, attrs)
			}
			switch span.SpanKind {
			case trace.SpanKindClient:
				clients++
				if attrs["peer.service"] != target {
					t.Errorf("client peer is not its business target: %v", attrs)
				}
			case trace.SpanKindServer:
				servers++
				if attrs["lumo.action"] != action {
					t.Errorf("server action is not its own request: %v", attrs)
				}
			}
			continue
		}
		business++
		if attrs["lumo.action"] != action || (!strings.HasSuffix(span.Name, "."+target+"."+action)) {
			t.Errorf("business span inherited another hop's target: %s %v", span.Name, attrs)
		}
	}
	if clients != 2 || servers != 2 || business != 4 {
		t.Errorf("missing hop spans: client=%d server=%d business=%d", clients, servers, business)
	}
}

func TestRPCTraceBoundsInvalidActionNames(t *testing.T) {
	exporter, _ := tracing(t)
	addr := rpcTraceServer(t, Module{Name: "catalog", Handle: func(context.Context, Command) (Result, error) {
		t.Error("invalid action reached domain handler")
		return nil, nil
	}})
	caller := NewCaller(map[string]string{"catalog": addr}, "trace-internal-token")
	t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
	invalid := strings.Repeat("unbounded-input", 100)
	if _, err := caller.Call(context.Background(), "catalog", Command{Action: invalid}); status.Code(
		err,
	) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	for _, span := range awaitRPCTraceSpans(t, exporter, 4) {
		if strings.Contains(span.Name, invalid) {
			t.Errorf("invalid action exported as span name")
		}
		attrs := rpcSpanAttributes(span)
		if attrs["lumo.target_service"] != "catalog" {
			t.Errorf("invalid action lost target on %s", span.Name)
		}
		if action := attrs["lumo.action"]; action != "" && action != "unknown" {
			t.Errorf("invalid action exported in %s: %q", span.Name, action)
		}
	}
}
