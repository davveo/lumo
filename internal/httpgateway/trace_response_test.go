package httpgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHTTPResponsesExposeTraceOnlyInHeader(t *testing.T) {
	_ = httpPayloadExporter(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://allowed.test")
	for _, tc := range []struct {
		name, method, path, body, origin string
		dependencyDown                   bool
		want                             int
	}{
		{name: "health", method: http.MethodGet, path: "/healthz", want: 200},
		{name: "ready", method: http.MethodGet, path: "/readyz", want: 200},
		{name: "unavailable dependency", method: http.MethodGet, path: "/readyz", dependencyDown: true, want: 503},
		{name: "success", method: http.MethodPost, path: "/api/v1/catalog/get", body: `{"id":"sku-public"}`, want: 200},
		{name: "not found", method: http.MethodPost, path: "/api/v1/catalog/get", body: `{"id":"missing"}`, want: 404},
		{name: "internal error", method: http.MethodPost, path: "/api/v1/catalog/get", body: `{"id":"broken"}`, want: 500},
		{name: "invalid JSON", method: http.MethodPost, path: "/api/v1/catalog/get", body: `{`, want: 400},
		{name: "unauthenticated", method: http.MethodPost, path: "/api/v1/member/get", body: `{}`, want: 401},
		{name: "unexposed action", method: http.MethodPost, path: "/api/v1/inventory/reserve", body: `{}`, want: 403},
		{name: "wrong method", method: http.MethodGet, path: "/api/v1/checkout/submit", want: 400},
		{name: "CORS rejection", method: http.MethodPost, path: "/api/v1/catalog/get", body: `{}`, origin: "http://denied.test", want: 403},
		{name: "CORS preflight", method: http.MethodOptions, path: "/api/v1/catalog/get", origin: "http://allowed.test", want: 204},
		{name: "recent aggregate", method: http.MethodPost, path: "/api/v1/storefront/recent", body: `{"data":{}}`, want: 200},
		{name: "debug console", method: http.MethodGet, path: "/debug", want: 200},
		{name: "unknown static route", method: http.MethodGet, path: "/missing.js", want: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := traceTestCaller(func(_ context.Context, _ string, cmd platform.Command) (platform.Result, error) {
				if tc.dependencyDown {
					return nil, status.Error(codes.Unavailable, "dependency unavailable")
				}
				switch cmd.ID {
				case "missing":
					return nil, status.Error(codes.NotFound, "product not found")
				case "broken":
					return nil, errors.New("private internal failure")
				}
				return platform.Result{"id": cmd.ID, "items": []any{}}, nil
			})
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			if tc.method == http.MethodOptions {
				request.Header.Set("Access-Control-Request-Method", "POST")
				request.Header.Set("Access-Control-Request-Headers", "content-type")
			}
			response := httptest.NewRecorder()
			NewHandler(caller, false, "").ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.want, response.Body)
			}
			id, err := trace.TraceIDFromHex(response.Header().Get("x-trace-id"))
			if err != nil || !id.IsValid() {
				t.Fatalf("missing valid trace response header: %v", response.Header())
			}
			if strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
				var envelope map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if _, exists := envelope["trace_id"]; exists {
					t.Fatalf("trace metadata leaked into the HTTP envelope: %s", response.Body)
				}
			}
			if tc.name == "CORS preflight" &&
				!strings.Contains(
					strings.ToLower(response.Header().Get("Access-Control-Expose-Headers")),
					"x-trace-id",
				) {
				t.Fatal("browser cannot read the trace response header")
			}
		})
	}
}

func TestHTTPJSONResponseDoesNotAddTraceMetadataToBusinessMap(t *testing.T) {
	payload := map[string]any{"data": map[string]any{"id": "sku-public"}}
	response := httptest.NewRecorder()
	writeJSON(response, context.Background(), http.StatusOK, payload)
	if len(payload) != 1 {
		t.Fatalf("response writer changed the supplied business envelope: %v", payload)
	}
	if response.Body.String() != "{\"data\":{\"id\":\"sku-public\"}}\n" {
		t.Fatal(response.Body.String())
	}
}
