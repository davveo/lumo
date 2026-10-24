package httpgateway

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
	"net/url"
	"strings"
)

// Parse exact origins only. A wildcard, URL path, credential, query or fragment
// never grants browser access. The default empty set keeps the API same-origin.
func corsOrigins(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		u, err := url.Parse(origin)
		if err != nil || u == nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") || strings.Contains(origin, "*") {
			continue
		}
		if u.Scheme+"://"+u.Host != origin {
			continue
		}
		out[origin] = true
	}
	return out
}

var corsHeaders = map[string]bool{"authorization": true, "content-type": true, "idempotency-key": true, "traceparent": true, "tracestate": true}

// applyCORS returns false when it has answered a preflight or denied an origin.
// Bearer credentials are explicit headers; cookie credentials are never enabled.
func applyCORS(w http.ResponseWriter, r *http.Request, allowed map[string]bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	same := origin == scheme+"://"+r.Host
	if !same && !allowed[origin] {
		writeError(w, r.Context(), status.Error(codes.PermissionDenied, "browser origin is not allowed"))
		return false
	}
	if r.Method == http.MethodOptions {
		method := r.Header.Get("Access-Control-Request-Method")
		if method != "GET" && method != "POST" {
			writeError(w, r.Context(), status.Error(codes.PermissionDenied, "CORS method is not allowed"))
			return false
		}
		for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
			header = strings.ToLower(strings.TrimSpace(header))
			if header != "" && !corsHeaders[header] {
				writeError(w, r.Context(), status.Error(codes.PermissionDenied, "CORS request header is not allowed"))
				return false
			}
		}
	}
	w.Header().Add("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Expose-Headers", "X-Trace-ID")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, traceparent, tracestate")
		w.Header().Set("Access-Control-Max-Age", "600")
		traceHTTPResponse(r.Context(), map[string]any{"status": http.StatusNoContent}, nil)
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	return true
}
