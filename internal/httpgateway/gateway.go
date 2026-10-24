package httpgateway

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// An HTTP action must appear here. In particular, internal money/resource/state
// commands cannot be accessed by choosing arbitrary service/action URL values.
var publicActions = actionSet(map[string]string{
	"iam": "login", "catalog": "list get batch_get", "search": "query", "recommendation": "list", "content": "list get", "inventory": "get batch_get", "campaign": "list evaluate", "group": "list get", "flash": "list", "interaction": "list", "logistics": "quote", "helpdesk": "faq", "coupon": "templates templates_snapshot", "storefront": "bootstrap batch",
})
var userActions = actionSet(map[string]string{
	"iam": "current refresh logout logout_all", "member": "get update", "address": "list get create update delete default", "cart": "list add set remove clear merge", "engagement": "list put delete clear", "media": "create get complete download", "interaction": "create like", "analytics": "record", "pricing": "quote get", "checkout": "quote submit cancel", "order": "list get complete", "payment": "create get submit_demo", "refund": "get", "coupon": "list claim", "points": "get checkin", "group": "create", "flash": "request get", "fulfillment": "get", "logistics": "get", "aftersale": "list get create return cancel", "invoice": "list get reversals create", "notification": "list read read_all", "helpdesk": "list get create reply close",
})
var adminActions = actionSet(map[string]string{
	"catalog": "upsert", "content": "upsert", "inventory": "adjust", "interaction": "moderate answer", "fulfillment": "list dispatch cancel inspect_return", "logistics": "deliver", "aftersale": "approve reject inspect refund exchange", "payment": "list confirm_demo", "refund": "list confirm_demo", "invoice": "issue_demo adjust_demo", "reconciliation": "list run", "analytics": "list",
})
var reads = map[string]bool{"list": true, "get": true, "batch_get": true, "query": true, "evaluate": true, "templates": true, "templates_snapshot": true, "current": true, "faq": true}

func actionSet(m map[string]string) map[string]bool {
	r := map[string]bool{}
	for service, actions := range m {
		for _, a := range strings.Fields(actions) {
			r[service+"/"+a] = true
		}
	}
	return r
}

type gateway struct {
	rpc  platform.Caller
	demo bool
}

type httpTraceContextKey struct{}

// Keep references to the HTTP and gateway spans so one parsed API response is
// recorded consistently at each boundary without buffering a ResponseWriter.
type httpTraceCall struct{ contexts []context.Context }

func beginHTTPTrace(ctx context.Context) context.Context {
	call := &httpTraceCall{contexts: []context.Context{ctx}}
	return context.WithValue(ctx, httpTraceContextKey{}, call)
}

func trackHTTPTrace(ctx context.Context) {
	if call, ok := ctx.Value(httpTraceContextKey{}).(*httpTraceCall); ok {
		call.contexts = append(call.contexts, ctx)
	}
}

func traceHTTPRequest(ctx context.Context, value any) {
	if call, ok := ctx.Value(httpTraceContextKey{}).(*httpTraceCall); ok {
		for _, boundary := range call.contexts {
			platform.TraceRequest(boundary, value)
		}
		return
	}
	platform.TraceRequest(ctx, value)
}

func traceHTTPResponse(ctx context.Context, value any, err error) {
	if call, ok := ctx.Value(httpTraceContextKey{}).(*httpTraceCall); ok {
		for _, boundary := range call.contexts {
			platform.TraceResponse(boundary, value, err)
		}
		return
	}
	platform.TraceResponse(ctx, value, err)
}

// NewHandler serves same-origin storefront assets and the authenticated HTTP API.
func NewHandler(rpc platform.Caller, demo bool, frontendDir string) http.Handler {
	g := &gateway{rpc: rpc, demo: demo}
	allowedOrigins := corsOrigins(os.Getenv("CORS_ALLOWED_ORIGINS"))
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r.Context(), http.StatusOK, map[string]any{"status": "ok", "service": "gateway"})
	})
	mux.HandleFunc("/readyz", g.ready)
	mux.HandleFunc("/api/v1/", g.execute)
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/debug" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, consoleHTML)
	})
	if frontendDir == "" {
		frontendDir = "web/dist"
	}
	files := http.FileServer(http.Dir(frontendDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Never expose directory listings or executable/private project files.
		clean := filepath.Clean("/" + r.URL.Path)
		if strings.Contains(clean, "/.") {
			http.NotFound(w, r)
			return
		}
		if clean == "/" {
			clean = "/index.html"
		}
		info, err := os.Stat(filepath.Join(frontendDir, strings.TrimPrefix(clean, "/")))
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
	return otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			r = r.WithContext(beginHTTPTrace(r.Context()))
			traceHTTPRequest(r.Context(), map[string]any{"method": r.Method, "body": "not parsed"})
		}
		trace.SpanFromContext(r.Context()).SetAttributes(apiTraceAttributes(r.URL.Path)...)
		tid := trace.SpanContextFromContext(r.Context()).TraceID().String()
		w.Header().Set("X-Trace-ID", tid)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		// API tokens are kept in memory by the small console. No cookie auth or
		// permissive cross-origin credentials are enabled by this gateway.
		if strings.HasPrefix(r.URL.Path, "/api/v1/") && !applyCORS(w, r, allowedOrigins) {
			return
		}
		mux.ServeHTTP(w, r)
	}), "gateway.http", otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + routeName(r.URL.Path) }))
}

// Only registered routes become business trace attributes. Never derive these
// tags from request headers or payloads, or accept arbitrary URL values.
func apiTraceAttributes(path string) []attribute.KeyValue {
	if !strings.HasPrefix(path, "/api/v1/") {
		return nil
	}
	key := strings.TrimPrefix(path, "/api/v1/")
	if !publicActions[key] && !userActions[key] && !adminActions[key] {
		return nil
	}
	service, action, _ := strings.Cut(key, "/")
	return []attribute.KeyValue{attribute.String("lumo.target_service", service), attribute.String("lumo.action", action)}
}

func routeName(path string) string {
	if strings.HasPrefix(path, "/api/v1/") {
		p := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
		if len(p) == 2 {
			return "/api/v1/" + p[0] + "/" + p[1]
		}
		return "/api/v1/{service}/{action}"
	}
	return path
}

func (g *gateway) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	// Public catalog read also confirms an RPC connection and a migrated DB.
	_, err := g.rpc.Call(ctx, "catalog", platform.Command{Action: "list", Data: map[string]any{"limit": 1}})
	if err != nil {
		writeError(w, ctx, status.Error(codes.Unavailable, "catalog dependency not ready"))
		return
	}
	writeJSON(w, ctx, http.StatusOK, map[string]any{"status": "ready"})
}

func (g *gateway) execute(w http.ResponseWriter, r *http.Request) {
	ctx, sp := platform.Start(r.Context(), "gateway.transport.Dispatch")
	attrs := apiTraceAttributes(r.URL.Path)
	sp.SetAttributes(attrs...)
	trackHTTPTrace(ctx)
	platform.TraceRequest(ctx, map[string]any{"method": r.Method, "body": "not parsed"})
	defer sp.End()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeError(w, ctx, status.Error(codes.NotFound, "unknown API route"))
		return
	}
	service, action := parts[0], parts[1]
	key := service + "/" + action
	pub, usr, admin := publicActions[key], userActions[key], adminActions[key]
	if !pub && !usr && !admin {
		writeError(w, ctx, status.Error(codes.PermissionDenied, "action is not exposed by the HTTP gateway"))
		return
	}
	if r.Method != http.MethodPost && (r.Method != http.MethodGet || !reads[action]) {
		w.Header().Set("Allow", "POST")
		writeError(w, ctx, status.Error(codes.InvalidArgument, "use POST for this action"))
		return
	}
	var cmd platform.Command
	if r.Method == http.MethodGet {
		cmd.ID = r.URL.Query().Get("id")
		cmd.Data = map[string]any{}
		for k, v := range r.URL.Query() {
			if k != "id" && len(v) > 0 {
				cmd.Data[k] = v[0]
			}
		}
	} else {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.UseNumber()
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cmd); err != nil && err != io.EOF {
			traceHTTPRequest(ctx, map[string]any{"body": "invalid JSON command"})
			writeError(w, ctx, status.Error(codes.InvalidArgument, "body must be one JSON command"))
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			traceHTTPRequest(ctx, map[string]any{"body": "invalid JSON command"})
			writeError(w, ctx, status.Error(codes.InvalidArgument, "body contains trailing data"))
			return
		}
	}
	cmd.Action = action
	cmd.UserID = ""
	cmd.Role = "" // never accept browser principal claims
	if cmd.Data == nil {
		cmd.Data = map[string]any{}
	}
	if v := r.Header.Get("Idempotency-Key"); v != "" {
		cmd.IdempotencyKey = v
	}
	// Only trace parsed values. Browser identity claims have already been
	// cleared, and the shared recorder redacts secrets without mutating cmd.
	traceHTTPRequest(ctx, cmd)
	token := ""
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		token = strings.TrimPrefix(auth, "Bearer ")
	}
	if !pub || token != "" {
		if token == "" {
			writeError(w, ctx, status.Error(codes.Unauthenticated, "Bearer token required"))
			return
		}
		authCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		principal, err := g.rpc.Call(authCtx, "iam", platform.Command{Action: "validate", Data: map[string]any{"token": token}})
		cancel()
		if err != nil {
			writeError(w, ctx, err)
			return
		}
		cmd.UserID = platform.Text(principal, "user_id")
		cmd.Role = platform.Text(principal, "role")
		if cmd.UserID == "" || cmd.Role == "system" {
			writeError(w, ctx, status.Error(codes.Unauthenticated, "invalid browser principal"))
			return
		}
		if admin && cmd.Role != "admin" {
			writeError(w, ctx, status.Error(codes.PermissionDenied, "administrator required"))
			return
		}
	}
	if strings.HasSuffix(action, "_demo") {
		if !g.demo {
			writeError(w, ctx, status.Error(codes.PermissionDenied, "demo provider actions are disabled"))
			return
		}
	}
	if service == "iam" && (action == "logout" || action == "refresh" || action == "current") {
		cmd.Data["token"] = token
	}
	if service == "iam" && action == "current" {
		cmd.Action = "validate"
	}
	ctx, sp2 := platform.Start(ctx, "gateway.usecase.Forward")
	sp2.SetAttributes(attrs...)
	trackHTTPTrace(ctx)
	platform.TraceRequest(ctx, cmd)
	defer sp2.End()
	callCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var result platform.Result
	var err error
	if service == "storefront" {
		result, err = g.storefront(callCtx, cmd)
	} else {
		result, err = g.rpc.Call(callCtx, service, cmd)
	}
	if err != nil {
		writeError(w, ctx, err)
		return
	}
	code := http.StatusOK
	if service == "flash" && action == "request" {
		code = http.StatusAccepted
	}
	writeJSON(w, ctx, code, map[string]any{"data": result})
}

func writeJSON(w http.ResponseWriter, ctx context.Context, code int, payload map[string]any) {
	writeJSONResponse(w, ctx, code, payload, nil)
}

func writeJSONResponse(w http.ResponseWriter, ctx context.Context, code int, payload map[string]any, err error) {
	payload["trace_id"] = trace.SpanContextFromContext(ctx).TraceID().String()
	if err == nil {
		traceHTTPResponse(ctx, payload, nil)
	} else {
		// Service errors may interpolate user input. Preserve the HTTP contract,
		// while recording only the canonical error code in tracing payloads.
		traceHTTPResponse(ctx, map[string]any{"error": map[string]any{"code": status.Code(err).String()}}, err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}
func writeError(w http.ResponseWriter, ctx context.Context, err error) {
	s, ok := status.FromError(err)
	if !ok {
		s = status.New(codes.Internal, "internal service error")
	}
	code := http.StatusInternalServerError
	switch s.Code() {
	case codes.InvalidArgument:
		code = 400
	case codes.Unauthenticated:
		code = 401
	case codes.PermissionDenied:
		code = 403
	case codes.NotFound:
		code = 404
	case codes.AlreadyExists, codes.Aborted, codes.FailedPrecondition:
		code = 409
	case codes.ResourceExhausted:
		code = 429
	case codes.Unimplemented:
		code = 501
	case codes.Unavailable:
		code = 503
	case codes.DeadlineExceeded:
		code = 504
	}
	writeJSONResponse(w, ctx, code, map[string]any{"error": map[string]any{"code": s.Code().String(), "message": s.Message()}}, status.Error(s.Code(), s.Code().String()))
}
