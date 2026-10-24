package inventory_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInventoryTransportAndUsecaseTraceRecordSuccessfulAndFailedCalls(t *testing.T) {
	_, module := setup(t, nil)
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	command := platform.Command{
		Action: "get",
		ID:     "sku-phone",
		Data:   map[string]any{"password": "inventory-trace-private-value"},
	}
	result, err := module.Handle(context.Background(), command)
	if err != nil || platform.Int(result, "available") != 50 {
		t.Fatalf("public inventory read changed: %v %v", result, err)
	}
	command.ID = "missing-stock-for-trace"
	if _, err = module.Handle(context.Background(), command); status.Code(err) != codes.NotFound {
		t.Fatalf("not-found behavior changed: %v", err)
	}
	counts := map[string]int{}
	for _, span := range exporter.GetSpans() {
		if span.Name != "transport.inventory.get" && span.Name != "usecase.inventory.get" {
			continue
		}
		counts[span.Name]++
		attributes := map[string]string{}
		for _, attribute := range span.Attributes {
			attributes[string(attribute.Key)] = attribute.Value.AsString()
			if strings.Contains(attribute.Value.AsString(), "inventory-trace-private-value") {
				t.Fatalf("request secret leaked in %s", span.Name)
			}
		}
		request, response := attributes["lumo.request"], attributes["lumo.response"]
		if !json.Valid([]byte(request)) || !json.Valid([]byte(response)) {
			t.Fatalf("missing JSON request/response on %s: %v", span.Name, attributes)
		}
		if strings.Contains(request, "missing-stock-for-trace") {
			if attributes["lumo.error.code"] != codes.NotFound.String() {
				t.Fatalf("not-found code missing from %s", span.Name)
			}
		} else if !strings.Contains(
			request,
			"sku-phone",
		) || !strings.Contains(
			response,
			`"available":50`,
		) || attributes["lumo.error.code"] != codes.OK.String() {
			t.Fatalf("successful values missing from %s: %v", span.Name, attributes)
		}
	}
	if counts["transport.inventory.get"] != 2 || counts["usecase.inventory.get"] != 2 {
		t.Fatalf("a business layer was omitted: %v", counts)
	}
}
