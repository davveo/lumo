package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/status"
)

type traceErrorContextKey struct{}
type traceErrorContext struct {
	mu       sync.Mutex
	values   map[string]struct{}
	prefixes map[string]struct{}
	bytes    int
	recorded map[trace.SpanID]string
}

// NewTraceErrorContext isolates redaction memory at an HTTP, server RPC or
// message boundary. Values remain in request memory and are never exported.
func NewTraceErrorContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, traceErrorContextKey{}, &traceErrorContext{values: map[string]struct{}{}, recorded: map[trace.SpanID]string{}})
}
func withTraceErrorContext(ctx context.Context) context.Context {
	if ctx.Value(traceErrorContextKey{}) == nil {
		return NewTraceErrorContext(ctx)
	}
	return ctx
}
func errorContext(ctx context.Context) *traceErrorContext {
	v, _ := ctx.Value(traceErrorContextKey{}).(*traceErrorContext)
	return v
}
func (c *traceErrorContext) remember(value string) {
	if c == nil || len(value) < 3 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(value) > 8192 {
		prefix := validPrefix(value, 128)
		if len(c.values)+len(c.prefixes) < 256 && c.bytes+len(prefix) <= 32768 {
			if c.prefixes == nil {
				c.prefixes = map[string]struct{}{}
			}
			if _, ok := c.prefixes[prefix]; !ok {
				c.prefixes[prefix] = struct{}{}
				c.bytes += len(prefix)
			}
		}
		return
	}
	if _, found := c.values[value]; found {
		return
	}
	if len(c.values) >= 256 || c.bytes+len(value) > 32768 {
		return
	}
	c.values[value] = struct{}{}
	c.bytes += len(value)
}
func (c *traceErrorContext) reflectValue(value reflect.Value, depth int, remaining *int) {
	if c == nil || depth > 8 || *remaining <= 0 {
		return
	}
	*remaining--
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return
		}
		value = value.Elem()
		depth++
		if depth > 8 {
			return
		}
	}
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.String:
		c.remember(value.String())
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() && *remaining > 0 {
			c.reflectValue(iterator.Value(), depth+1, remaining)
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			if value.Len() <= 8192 && value.Kind() == reflect.Slice {
				c.remember(string(value.Bytes()))
			}
			return
		}
		for i := 0; i < value.Len() && *remaining > 0; i++ {
			c.reflectValue(value.Index(i), depth+1, remaining)
		}
	case reflect.Struct:
		for i := 0; i < value.NumField() && *remaining > 0; i++ {
			if value.Type().Field(i).PkgPath == "" {
				c.reflectValue(value.Field(i), depth+1, remaining)
			}
		}
	}
}

var (
	errorURL         = regexp.MustCompile(`(?i)(?:https?|amqps?|rediss?|mysql|postgres(?:ql)?)://[^\s<>"']+`)
	errorAssignment  = regexp.MustCompile(`(?i)((?:password|passwd|pwd|passphrase|secret|token|credential|authorization|cookie|api[_-]?key|access[_-]?key|private[_-]?key|phone|mobile|email|address|recipient|username|user_id|密码|口令|密钥|手机号|地址)["'\\]*\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;}&]+)`)
	errorDSN         = regexp.MustCompile(`[^\s:@/]+:[^\s@]+@(?:tcp|unix)\([^)]*\)`)
	errorPEM         = regexp.MustCompile(`(?s)-----BEGIN [^-]+-----.*?(?:-----END [^-]+-----|$)`)
	errorOpaque      = regexp.MustCompile(`\b(?:[a-fA-F0-9]{40,}|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)\b`)
	errorDuplicate   = regexp.MustCompile(`(?i)(duplicate entry\s+)'(?:''|[^'])*'`)
	errorSQLFragment = regexp.MustCompile(`(?i)(near\s+)'(?:''|[^'])*'`)
)

func safeTraceErrorText(ctx context.Context, value string) (string, bool, bool) {
	truncated := len(value) > 8192
	value = validPrefix(strings.ToValidUTF8(value, "�"), 8192)
	value = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' || r == 127 {
			return ' '
		}
		return r
	}, value)
	original := value
	if c := errorContext(ctx); c != nil {
		c.mu.Lock()
		values := make([]string, 0, len(c.values))
		prefixes := make([]string, 0, len(c.prefixes))
		for item := range c.values {
			values = append(values, item)
		}
		for item := range c.prefixes {
			prefixes = append(prefixes, item)
		}
		c.mu.Unlock()
		sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
		for _, secret := range values {
			if len(secret) >= 32 {
				if index := strings.Index(value, secret[:32]); index >= 0 && strings.HasPrefix(secret, value[index:]) {
					value = value[:index] + traceRedacted
				}
			}
			value = strings.ReplaceAll(value, secret, traceRedacted)
		}
		for _, prefix := range prefixes {
			if index := strings.Index(value, prefix); index >= 0 {
				value = value[:index] + traceRedacted
			}
		}
	}
	value = errorPEM.ReplaceAllString(value, traceRedacted)
	value = errorDSN.ReplaceAllString(value, "[REDACTED]@database")
	value = errorURL.ReplaceAllStringFunc(value, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return traceRedacted
		}
		if u.User != nil {
			u.User = url.User(traceRedacted)
		}
		if u.RawQuery != "" {
			u.RawQuery = "[REDACTED]"
		}
		if u.Fragment != "" {
			u.Fragment = "[REDACTED]"
		}
		return u.String()
	})
	value = errorAssignment.ReplaceAllString(value, "${1}"+traceRedacted)
	value = traceBearer.ReplaceAllString(value, "Bearer "+traceRedacted)
	value = traceEmail.ReplaceAllString(value, traceRedacted)
	value = errorOpaque.ReplaceAllString(value, traceRedacted)
	value = errorDuplicate.ReplaceAllString(value, "${1}'"+traceRedacted+"'")
	value = errorSQLFragment.ReplaceAllString(value, "${1}'[SQL FRAGMENT REDACTED]'")
	var phoneSafe strings.Builder
	previous := 0
	identifier := func(char byte) bool {
		return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_'
	}
	for _, match := range tracePhone.FindAllStringIndex(value, -1) {
		if match[0] > 0 && identifier(value[match[0]-1]) || match[1] < len(value) && identifier(value[match[1]]) {
			continue
		}
		phoneSafe.WriteString(value[previous:match[0]])
		phoneSafe.WriteString(traceRedacted)
		previous = match[1]
	}
	phoneSafe.WriteString(value[previous:])
	value = phoneSafe.String()
	redacted := original != value
	if len(value) > 2048 {
		value = validPrefix(value, 2048) + "…"
		truncated = true
	}
	return value, redacted, truncated
}

func errorText(err error) string {
	if s, ok := status.FromError(err); ok {
		return s.Message()
	}
	return err.Error()
}

func recordedTraceError(ctx context.Context) string {
	c := errorContext(ctx)
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recorded[trace.SpanContextFromContext(ctx).SpanID()]
}
func errorCallSite() []attribute.KeyValue {
	pcs := make([]uintptr, 12)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if !strings.HasSuffix(frame.Function, ".TraceError") && !strings.HasSuffix(frame.Function, ".TraceResponse") {
			path := frame.File
			if i := strings.Index(path, "/lumo/"); i >= 0 {
				path = path[i+6:]
			}
			return []attribute.KeyValue{attribute.String("code.file.path", path), attribute.Int("code.line.number", frame.Line), attribute.String("code.function.name", frame.Function)}
		}
		if !more {
			break
		}
	}
	return nil
}

// TraceError records sanitized details before transport conversions can replace
// the original failure. It does not change the error returned to the caller.
func TraceError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if err == nil || !span.IsRecording() {
		return
	}
	message, redacted, truncated := safeTraceErrorText(ctx, errorText(err))
	code := traceErrorCode(err).String()
	chain := []map[string]string{}
	queue := []error{err}
	for len(queue) > 0 && len(chain) < 8 {
		current := queue[0]
		queue = queue[1:]
		text, r, t := safeTraceErrorText(ctx, errorText(current))
		redacted = redacted || r
		truncated = truncated || t
		if len(text) > 512 {
			text = validPrefix(text, 512) + "…"
			truncated = true
		}
		chain = append(chain, map[string]string{"type": fmt.Sprintf("%T", current), "message": text})
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			for _, child := range joined.Unwrap() {
				if child != nil && len(queue) < 8 {
					queue = append(queue, child)
				}
			}
		} else if child := errors.Unwrap(current); child != nil {
			queue = append(queue, child)
		}
	}
	if len(queue) > 0 {
		truncated = true
	}
	encoded, _ := json.Marshal(chain)
	first := true
	if c := errorContext(ctx); c != nil {
		c.mu.Lock()
		id := span.SpanContext().SpanID()
		if c.recorded[id] != "" {
			first = false
		} else if len(c.recorded) < 512 {
			c.recorded[id] = message
		}
		c.mu.Unlock()
	}
	if first {
		span.SetAttributes(attribute.String("lumo.error.code", code), attribute.String("lumo.error.message", message), attribute.String("error.message", message), attribute.String("error.type", fmt.Sprintf("%T", err)), attribute.String("lumo.error.causes", string(encoded)), attribute.Bool("lumo.error.redacted", redacted), attribute.Bool("lumo.error.truncated", truncated))
		span.SetStatus(otelcodes.Error, code+": "+message)
	}
	attrs := append(errorCallSite(), attribute.String("exception.type", fmt.Sprintf("%T", err)), attribute.String("exception.message", message), attribute.Bool("exception.escaped", false))
	span.AddEvent("exception", trace.WithAttributes(attrs...))
}
