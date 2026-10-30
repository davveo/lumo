package platform

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type backgroundTraceKey struct{}

// A poll is only idle if it finds no rows, calls no external service and has no
// error. Keep the whole local trace when it does work, including the queries
// which found the work. Interactive requests never enter this path.
type backgroundTrace struct {
	mu       sync.Mutex
	pending  []sdktrace.ReadOnlySpan
	keep     bool
	finished bool
}

const maxBackgroundTraceSpans = 64

func (b *backgroundTrace) end(next sdktrace.SpanProcessor, span sdktrace.ReadOnlySpan) {
	b.mu.Lock()
	if meaningfulBackgroundSpan(span) {
		b.keep = true
	}
	// Fail open on a large poll or late asynchronous work. A slow exporter must
	// never cause an unbounded buffer while waiting for the poll to finish.
	if len(b.pending) >= maxBackgroundTraceSpans || b.finished {
		b.keep = true
	}
	if b.keep {
		for _, pending := range b.pending {
			next.OnEnd(pending)
		}
		b.pending = nil
		next.OnEnd(span)
	} else {
		b.pending = append(b.pending, span)
	}
	b.mu.Unlock()
}

func (b *backgroundTrace) finish() {
	b.mu.Lock()
	b.finished = true
	b.pending = nil
	b.mu.Unlock()
}

func meaningfulBackgroundSpan(span sdktrace.ReadOnlySpan) bool {
	if span.Status().Code == codes.Error || len(span.Events()) > 0 {
		return true
	}
	database := false
	for _, attr := range span.Attributes() {
		switch attr.Key {
		case "db.system.name":
			database = true
		case "db.response.returned_rows":
			if attr.Value.AsInt64() > 0 {
				return true
			}
		}
	}
	switch span.SpanKind() {
	case trace.SpanKindClient:
		return !database
	case trace.SpanKindProducer, trace.SpanKindConsumer, trace.SpanKindServer:
		return true
	}
	return false
}

type backgroundSpanProcessor struct {
	next   sdktrace.SpanProcessor
	mu     sync.Mutex
	active map[trace.SpanID]*backgroundTrace
}

func newBackgroundSpanProcessor(next sdktrace.SpanProcessor) *backgroundSpanProcessor {
	return &backgroundSpanProcessor{next: next, active: make(map[trace.SpanID]*backgroundTrace)}
}

func (p *backgroundSpanProcessor) OnStart(ctx context.Context, span sdktrace.ReadWriteSpan) {
	p.next.OnStart(ctx, span)
	if poll, ok := ctx.Value(backgroundTraceKey{}).(*backgroundTrace); ok {
		p.mu.Lock()
		// Overflow also fails open, keeping diagnostics without growing the map.
		if len(p.active) < 1024 {
			p.active[span.SpanContext().SpanID()] = poll
		}
		p.mu.Unlock()
	}
}

func (p *backgroundSpanProcessor) OnEnd(span sdktrace.ReadOnlySpan) {
	p.mu.Lock()
	id := span.SpanContext().SpanID()
	poll := p.active[id]
	delete(p.active, id)
	p.mu.Unlock()
	if poll == nil {
		p.next.OnEnd(span)
		return
	}
	poll.end(p.next, span)
}

func (p *backgroundSpanProcessor) Shutdown(ctx context.Context) error { return p.next.Shutdown(ctx) }
func (p *backgroundSpanProcessor) ForceFlush(ctx context.Context) error {
	return p.next.ForceFlush(ctx)
}

func traceBackgroundPoll(ctx context.Context, name string, work func(context.Context) error) (err error) {
	// A poll invoked by an existing business trace must remain in that trace.
	if trace.SpanContextFromContext(ctx).IsValid() {
		return work(ctx)
	}
	poll := &backgroundTrace{}
	ctx = context.WithValue(NewTraceErrorContext(ctx), backgroundTraceKey{}, poll)
	ctx, span := Start(ctx, name)
	defer poll.finish()
	defer span.End()
	defer func() { TraceError(ctx, err) }()
	return work(ctx)
}
