package order_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"github.com/davveo/lumo/services/order/internal/repository"
	"github.com/davveo/lumo/services/order/internal/usecase"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOrderUsecaseTraceRecordsTypedArgumentsAndReturnedValues(t *testing.T) {
	f := newOrderFixture(t)
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	service := usecase.New(repository.New(f.db), f.rpc)
	base := usecase.OrderRequest{Actor: usecase.Actor{UserID: "buyer", Role: "system"}, ID: "trace-order"}
	request := usecase.CreateOrderRequest{
		OrderRequest: base,
		Quote:        json.RawMessage(`{"payable_cents":1000,"password":"quote-secret-for-trace"}`),
		Address:      json.RawMessage(`{"phone":"13912345678","street":"trace-private-street"}`),
		Note:         "trace-private-note",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	}
	created, err := service.Create(context.Background(), request)
	if err != nil || created.ID != base.ID || created.Payable != 1000 || created.Note != request.Note {
		t.Fatalf("trace instrumentation changed creation: %+v %v", created, err)
	}
	_, err = service.Get(context.Background(), usecase.OrderRequest{Actor: usecase.Actor{UserID: "another-member", Role: "user"}, ID: base.ID})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ownership check changed: %v", err)
	}
	refunded, err := service.Refunded(context.Background(), usecase.RecordRefundRequest{OrderRequest: base, RefundID: "trace-refund-receipt", AmountCents: 100})
	if err != nil || refunded.Refunded != 100 {
		t.Fatalf("refund behavior changed: %+v %v", refunded, err)
	}
	spans := map[string]map[string]string{}
	for _, span := range exporter.GetSpans() {
		attributes := map[string]string{}
		for _, attribute := range span.Attributes {
			value := attribute.Value.AsString()
			for _, secret := range []string{"quote-secret-for-trace", "13912345678", "trace-private-street", "trace-private-note"} {
				if strings.Contains(value, secret) {
					t.Fatalf("private value leaked in %s/%s", span.Name, attribute.Key)
				}
			}
			attributes[string(attribute.Key)] = value
		}
		spans[span.Name] = attributes
	}
	for _, name := range []string{"order.usecase.create", "order.usecase.get", "order.usecase.refunded"} {
		attributes := spans[name]
		if !json.Valid([]byte(attributes["lumo.request"])) || !json.Valid([]byte(attributes["lumo.response"])) {
			t.Fatalf("missing bounded JSON payload on %s: %v", name, attributes)
		}
	}
	if response := spans["order.usecase.create"]["lumo.response"]; !strings.Contains(response, `"payable_cents":1000`) || !strings.Contains(response, `"status":"CREATING"`) {
		t.Fatalf("creation return was not recorded: %s", response)
	}
	if got := spans["order.usecase.get"]["lumo.error.code"]; got != codes.PermissionDenied.String() {
		t.Fatalf("failure code missing from usecase span: %q", got)
	}
	if request := spans["order.usecase.refunded"]["lumo.request"]; !strings.Contains(request, "trace-refund-receipt") || !strings.Contains(request, `"AmountCents":100`) {
		t.Fatalf("shared transition lost the complete refund request: %s", request)
	}
	if row := f.row(t, base.ID); row.Quote != string(request.Quote) || row.Address != string(request.Address) || row.Note != request.Note {
		t.Fatal("payload tracing mutated persisted historical snapshots")
	}
}

func TestOrderTypedTransportTraceIncludesInputsAndBusinessErrors(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	f := newOrderRPCFixture(t)
	request := rpcCreateRequest(t, "typed-trace-order", "buyer", "")
	if _, err := f.client.CreateOrder(f.ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.GetOrder(f.ctx, orderRequest(request.Id, "other", "user")); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-member typed call changed: %v", err)
	}
	if _, err := f.client.ListOrders(f.ctx, &mallv1.ListOrdersRequest{Actor: orderActor("buyer", "user")}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.AcceptPayment(f.ctx, &mallv1.AcceptPaymentRequest{Id: request.Id, Actor: orderActor("buyer", "system"), PaymentId: "typed-trace-payment", AmountCents: 1}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("typed payment mismatch validation changed: %v", err)
	}
	spans := map[string]map[string]string{}
	for _, span := range exporter.GetSpans() {
		if !strings.HasPrefix(span.Name, "order.transport.") {
			continue
		}
		attributes := map[string]string{}
		for _, attribute := range span.Attributes {
			attributes[string(attribute.Key)] = attribute.Value.AsString()
		}
		spans[span.Name] = attributes
	}
	for name, code := range map[string]codes.Code{"order.transport.create": codes.OK, "order.transport.get": codes.PermissionDenied, "order.transport.list": codes.OK, "order.transport.accept_payment": codes.InvalidArgument} {
		attributes := spans[name]
		if !json.Valid([]byte(attributes["lumo.request"])) || !json.Valid([]byte(attributes["lumo.response"])) || attributes["lumo.error.code"] != code.String() {
			t.Fatalf("typed transport payload/status missing from %s: %v", name, attributes)
		}
	}
	if input := spans["order.transport.accept_payment"]["lumo.request"]; !strings.Contains(input, "typed-trace-payment") || !strings.Contains(input, `"amount_cents":1`) {
		t.Fatalf("typed transport did not record actual payment arguments: %s", input)
	}
	if response := spans["order.transport.list"]["lumo.response"]; !strings.Contains(response, "typed-trace-order") {
		t.Fatalf("typed transport did not record order list return: %s", response)
	}
}
