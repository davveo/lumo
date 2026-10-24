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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func httpPayloadExporter(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	return exporter
}

func payloadSpanAttributes(span tracetest.SpanStub) map[string]string {
	attrs := map[string]string{}
	for _, attr := range span.Attributes {
		attrs[string(attr.Key)] = attr.Value.AsString()
	}
	return attrs
}

func assertHTTPPayloadPrivacy(t *testing.T, span tracetest.SpanStub) {
	t.Helper()
	encoded, _ := json.Marshal(span)
	for _, private := range []string{
		"private-browser-token",
		"private-input-password",
		"private-output-token",
		"private-user-id",
		"private-location",
		"13521373352",
	} {
		if strings.Contains(string(encoded), private) {
			t.Errorf("%s leaked %q", span.Name, private)
		}
	}
}

func TestHTTPTraceCapturesParsedRequestAndResponseAtEveryBoundary(t *testing.T) {
	exporter := httpPayloadExporter(t)
	called := false
	caller := traceTestCaller(func(_ context.Context, service string, cmd platform.Command) (platform.Result, error) {
		called = true
		if service != "catalog" || cmd.Action != "get" || cmd.ID != "sku-public" || cmd.UserID != "" || cmd.Role != "" {
			t.Fatalf("gateway command changed or trusted browser identity: %+v", cmd)
		}
		if platform.Text(cmd.Data, "password") != "private-input-password" ||
			platform.Text(cmd.Data, "query") != "coffee" {
			t.Fatal("tracing mutated the business request")
		}
		return platform.Result{
			"id":          "sku-public",
			"price_cents": 1599,
			"token":       "private-output-token",
			"user_id":     "private-user-id",
			"phone":       "13521373352",
			"address":     "private-location",
		}, nil
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/catalog/get",
		strings.NewReader(
			`{"id":"sku-public","user_id":"private-user-id","role":"admin","data":{"query":"coffee","password":"private-input-password","phone":"13521373352","address":"private-location"}}`,
		),
	)
	w := httptest.NewRecorder()
	NewHandler(caller, true, t.TempDir()).ServeHTTP(w, req)
	if w.Code != 200 || !called || !strings.Contains(w.Body.String(), "private-output-token") {
		t.Fatalf("business response was changed: %d %s", w.Code, w.Body.String())
	}
	want := map[string]bool{
		"POST /api/v1/catalog/get":   false,
		"gateway.transport.Dispatch": false,
		"gateway.usecase.Forward":    false,
	}
	for _, span := range exporter.GetSpans() {
		if _, ok := want[span.Name]; !ok {
			continue
		}
		want[span.Name] = true
		attrs := payloadSpanAttributes(span)
		if !strings.Contains(attrs["lumo.request"], "coffee") ||
			!strings.Contains(attrs["lumo.request"], "sku-public") {
			t.Errorf("%s missing business request: %v", span.Name, attrs)
		}
		if !strings.Contains(attrs["lumo.response"], "1599") ||
			!strings.Contains(attrs["lumo.response"], "sku-public") {
			t.Errorf("%s missing business response: %v", span.Name, attrs)
		}
		assertHTTPPayloadPrivacy(t, span)
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing recorded boundary %s", name)
		}
	}
}

func TestHTTPTraceErrorsKeepPayloadsAndDiscardRawErrorMessages(t *testing.T) {
	for _, test := range []struct {
		name, path, body, errorCode string
		status                      int
	}{
		{
			"invalid-json",
			"/api/v1/catalog/get",
			`{"data":{"password":"private-input-password"},invalid`,
			"InvalidArgument",
			400,
		},
		{
			"trailing-json",
			"/api/v1/catalog/get",
			`{"data":{"password":"private-input-password"}} {"password":"private-input-password"}`,
			"InvalidArgument",
			400,
		},
		{
			"missing-session",
			"/api/v1/order/get",
			`{"id":"ord-public","data":{"password":"private-input-password"}}`,
			"Unauthenticated",
			401,
		},
		{"unexposed", "/api/v1/order/ready", `{"data":{"password":"private-input-password"}}`, "PermissionDenied", 403},
		{
			"downstream-error",
			"/api/v1/catalog/get",
			`{"id":"sku-public","data":{"password":"private-input-password"}}`,
			"FailedPrecondition",
			409,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			exporter := httpPayloadExporter(t)
			caller := traceTestCaller(func(context.Context, string, platform.Command) (platform.Result, error) {
				return nil, status.Error(
					codes.FailedPrecondition,
					"private-input-password private-user-id private-location 13521373352",
				)
			})
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			w := httptest.NewRecorder()
			NewHandler(caller, true, t.TempDir()).ServeHTTP(w, req)
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, test.status, w.Body.String())
			}
			if test.name == "downstream-error" && !strings.Contains(w.Body.String(), "private-input-password") {
				t.Fatal("tracing changed the business error response")
			}
			for _, span := range exporter.GetSpans() {
				attrs := payloadSpanAttributes(span)
				if attrs["lumo.request"] == "" || attrs["lumo.response"] == "" ||
					attrs["lumo.error.code"] != test.errorCode {
					t.Errorf("%s missing request, response or safe error: %v", span.Name, attrs)
				}
				assertHTTPPayloadPrivacy(t, span)
			}
		})
	}
}

func TestHTTPTraceIncludesGETArgumentsAndStorefrontAggregateResult(t *testing.T) {
	for _, test := range []struct{ name, method, path, body string }{
		{"get", http.MethodGet, "/api/v1/catalog/get?id=sku-public&query=coffee&password=private-input-password", ""},
		{
			"aggregate",
			http.MethodPost,
			"/api/v1/storefront/batch",
			`{"data":{"requests":[{"key":"product","service":"catalog","action":"get","command":{"id":"sku-public","data":{"query":"coffee","password":"private-input-password"}}}]}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			exporter := httpPayloadExporter(t)
			caller := traceTestCaller(
				func(_ context.Context, service string, cmd platform.Command) (platform.Result, error) {
					if service != "catalog" || cmd.Action != "get" {
						t.Fatalf("unexpected aggregate request: %s/%s", service, cmd.Action)
					}
					return platform.Result{"id": "sku-public", "price_cents": 1599}, nil
				},
			)
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			w := httptest.NewRecorder()
			NewHandler(caller, true, t.TempDir()).ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			for _, span := range exporter.GetSpans() {
				attrs := payloadSpanAttributes(span)
				if !strings.Contains(attrs["lumo.request"], "coffee") ||
					!strings.Contains(attrs["lumo.response"], "1599") {
					t.Errorf("%s missing GET/aggregate payload: %v", span.Name, attrs)
				}
				assertHTTPPayloadPrivacy(t, span)
			}
		})
	}
}

func TestHTTPTraceRecordsPreflightWithoutReadingHeadersOrBody(t *testing.T) {
	exporter := httpPayloadExporter(t)
	req := httptest.NewRequest(
		http.MethodOptions,
		"http://api.local/api/v1/catalog/list",
		strings.NewReader("private-input-password"),
	)
	req.Header.Set("Origin", "http://api.local")
	req.Header.Set("Authorization", "Bearer private-browser-token")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	NewHandler(nil, true, t.TempDir()).ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("preflight response changed: %d %s", w.Code, w.Body.String())
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("preflight created business calls: %d", len(spans))
	}
	attrs := payloadSpanAttributes(spans[0])
	if attrs["lumo.request"] == "" || !strings.Contains(attrs["lumo.response"], "204") {
		t.Errorf("missing preflight request/response: %v", attrs)
	}
	assertHTTPPayloadPrivacy(t, spans[0])
}
