package httpgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type traceTestCaller func(context.Context, string, platform.Command) (platform.Result, error)

func (call traceTestCaller) Call(ctx context.Context, service string, cmd platform.Command) (platform.Result, error) {
	return call(ctx, service, cmd)
}

func TestHTTPTraceRecordsTargetWithoutChangingGatewayIdentity(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resource.NewWithAttributes("", attribute.String("service.name", "gateway"))),
	)
	previous, propagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(propagator)
	})

	for _, tc := range []struct {
		name, path, target, action, token, calls string
		status                                   int
	}{
		{"public", "/api/v1/catalog/list", "catalog", "list", "", "catalog/list", 200},
		{"authenticated", "/api/v1/member/get", "member", "get", "private-browser-token", "iam/validate member/get", 200},
		{"mapped-action", "/api/v1/iam/current", "iam", "current", "private-browser-token", "iam/validate iam/validate", 200},
		{"unauthenticated", "/api/v1/order/list", "order", "list", "", "", 401},
		{"unexposed", "/api/v1/inventory/reserve", "", "", "", "", 403},
		{"unknown", "/api/v1/private-route/unknown-action", "", "", "", "", 403},
		{"malformed", "/api/v1/catalog/list/extra", "", "", "", "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exporter.Reset()
			var calls []string
			const incomingTrace = "0123456789abcdef0123456789abcdef"
			caller := traceTestCaller(func(ctx context.Context, service string, cmd platform.Command) (platform.Result, error) {
				calls = append(calls, service+"/"+cmd.Action)
				if trace.SpanContextFromContext(ctx).TraceID().String() != incomingTrace {
					t.Error("HTTP trace context was not forwarded")
				}
				if service == "iam" {
					return platform.Result{"user_id": "private-user-id", "role": "user"}, nil
				}
				return platform.Result{"items": []any{}}, nil
			})
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{"data":{"secret":"private-request-body"}}`))
			req.Header.Set("traceparent", "00-"+incomingTrace+"-0123456789abcdef-01")
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			NewHandler(caller, true, t.TempDir()).ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			if strings.Join(calls, " ") != tc.calls {
				t.Fatalf("calls = %v, want %s", calls, tc.calls)
			}
			if w.Header().Get("X-Trace-ID") != incomingTrace {
				t.Fatal("response trace header does not match incoming context")
			}
			spans := exporter.GetSpans()
			if len(spans) < 2 {
				t.Fatalf("missing HTTP or dispatch spans: %v", spans)
			}
			for _, span := range spans {
				if span.SpanContext.TraceID().String() != incomingTrace {
					t.Errorf("split trace at %s", span.Name)
				}
				attrs := map[string]string{}
				for _, a := range span.Attributes {
					attrs[string(a.Key)] = a.Value.AsString()
				}
				if attrs["lumo.target_service"] != tc.target || attrs["lumo.action"] != tc.action {
					t.Errorf("%s route tags = %v; want %s/%s", span.Name, attrs, tc.target, tc.action)
				}
				service, ok := span.Resource.Set().Value(attribute.Key("service.name"))
				if !ok || service.AsString() != "gateway" {
					t.Errorf("%s changed the emitting service identity", span.Name)
				}
				encoded, _ := json.Marshal(span.Attributes)
				for _, private := range []string{"private-browser-token", "private-request-body", "private-user-id"} {
					if strings.Contains(string(encoded), private) {
						t.Errorf("private data %q exported by %s", private, span.Name)
					}
				}
			}
		})
	}
}
