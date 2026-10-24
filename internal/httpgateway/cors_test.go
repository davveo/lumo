package httpgateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExactOriginCORSPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, configured, origin, method, headers string
		code                                      int
		allow                                     bool
	}{
		{
			"allowlisted",
			"https://shop.example",
			"https://shop.example",
			"POST",
			"Authorization, Content-Type, Idempotency-Key, traceparent, tracestate",
			204,
			true,
		},
		{"unlisted", "https://shop.example", "https://evil.example", "POST", "Authorization", 403, false},
		{"defaultcrossorigin", "", "https://shop.example", "POST", "Authorization", 403, false},
		{"sameorigin", "", "http://api.local", "POST", "Authorization", 204, true},
		{"nowildcard", "*", "https://evil.example", "POST", "Authorization", 403, false},
		{"nopath", "https://shop.example/path", "https://shop.example", "POST", "Authorization", 403, false},
		{"methoddenied", "https://shop.example", "https://shop.example", "DELETE", "Authorization", 403, false},
		{"headerdenied", "https://shop.example", "https://shop.example", "POST", "X-Unapproved", 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_ORIGINS", tc.configured)
			h := NewHandler(nil, false, t.TempDir())
			req := httptest.NewRequest(http.MethodOptions, "http://api.local/api/v1/catalog/list", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Access-Control-Request-Method", tc.method)
			req.Header.Set("Access-Control-Request-Headers", tc.headers)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.code {
				t.Fatalf("got%d: %s", w.Code, w.Body.String())
			}
			origin := w.Header().Get("Access-Control-Allow-Origin")
			if tc.allow && origin != tc.origin || !tc.allow && origin != "" {
				t.Fatalf("unexpected allowedorigin %q", origin)
			}
			if w.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatal("cookie credentials enabled")
			}
			if tc.allow && !strings.Contains(w.Header().Get("Access-Control-Expose-Headers"), "X-Trace-ID") {
				t.Fatal("trace header is not exposed")
			}
		})
	}
}
