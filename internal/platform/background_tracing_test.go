package platform

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func backgroundTracing(t *testing.T) (*tracetest.InMemoryExporter, *backgroundSpanProcessor) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	processor := newBackgroundSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter))
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	return exporter, processor
}

func TestBackgroundPollingDropsOnlyIdleScansAndRetainsCompleteWork(t *testing.T) {
	exporter, _ := backgroundTracing(t)
	db := testDB(t)
	exporter.Reset()
	poll := func(ctx context.Context) error {
		ctx, span := Start(ctx, "test.repository.Pending")
		defer span.End()
		var rows []Outbox
		return db.WithContext(ctx).Where("state = ?", "pending").Find(&rows).Error
	}
	if err := runServiceTick(context.Background(), db, "test", poll); err != nil {
		t.Fatal(err)
	}
	if got := exporter.GetSpans(); len(got) != 0 {
		t.Fatal("idle SQL and parent spans were exported", got)
	}
	if err := db.Create(&Outbox{ID: "evt-background-test", State: "pending"}).Error; err != nil {
		t.Fatal(err)
	}
	exporter.Reset()
	if err := runServiceTick(context.Background(), db, "test", poll); err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatal("work must retain worker, repository and SQL spans", spans)
	}
	if spans[0].Parent.SpanID() != spans[1].SpanContext.SpanID() || spans[1].Parent.SpanID() != spans[2].SpanContext.SpanID() {
		t.Fatal("business work lost its trace hierarchy", spans)
	}
	// An empty query invoked by an interactive request must still be exported.
	exporter.Reset()
	ctx, parent := Start(context.Background(), "test.interactive")
	_ = db.WithContext(ctx).Where("state = ?", "absent").Find(&[]Outbox{}).Error
	parent.End()
	if len(exporter.GetSpans()) != 2 {
		t.Fatal("interactive tracing was filtered")
	}
}

func TestBackgroundPollingKeepsCaughtDatabaseFailuresAndExternalCalls(t *testing.T) {
	exporter, _ := backgroundTracing(t)
	db := testDB(t)
	exporter.Reset()
	err := runServiceTick(context.Background(), db, "test", func(ctx context.Context) error {
		// Even a swallowed SQL error must survive the idle filter.
		_ = db.WithContext(ctx).Exec("UPDATE absent_background_table SET value = ?", "private-value").Error
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 2 || !strings.Contains(rpcSpanAttributes(spans[0])["error.message"], "no such table: absent_background_table") {
		t.Fatal("caught database error was lost", spans)
	}
	exporter.Reset()
	_ = traceBackgroundPoll(context.Background(), "worker.rpc", func(ctx context.Context) error {
		_, span := otel.Tracer("test").Start(ctx, "external.operation", trace.WithSpanKind(trace.SpanKindClient))
		span.End()
		return nil
	})
	if len(exporter.GetSpans()) != 2 {
		t.Fatal("successful external work must retain its poll trace")
	}
}

func TestBackgroundPollingBoundsBuffersAndSeparatesConcurrentDecisions(t *testing.T) {
	exporter, processor := backgroundTracing(t)
	_ = traceBackgroundPoll(context.Background(), "worker.large", func(ctx context.Context) error {
		for range maxBackgroundTraceSpans + 2 {
			_, span := Start(ctx, "repository.scan")
			span.End()
			poll := ctx.Value(backgroundTraceKey{}).(*backgroundTrace)
			if len(poll.pending) > maxBackgroundTraceSpans {
				t.Fatal("background buffer is unbounded")
			}
		}
		return nil
	})
	if len(exporter.GetSpans()) != maxBackgroundTraceSpans+3 {
		t.Fatal("large polls must fail open and retain all spans")
	}
	exporter.Reset()
	var workers sync.WaitGroup
	for n := range 32 {
		workers.Go(func() {
			_ = traceBackgroundPoll(context.Background(), "worker.concurrent", func(ctx context.Context) error {
				_, span := Start(ctx, "repository.scan")
				span.End()
				if n%2 == 0 {
					return errors.New("scheduled work database unavailable")
				}
				return nil
			})
		})
	}
	workers.Wait()
	if len(exporter.GetSpans()) != 32 || len(processor.active) != 0 {
		t.Fatal("polls leaked spans or shared their export decisions", len(exporter.GetSpans()), len(processor.active))
	}
}
