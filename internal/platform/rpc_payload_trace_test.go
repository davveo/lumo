package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func assertRPCPayloadSpans(
	t *testing.T,
	spans tracetest.SpanStubs,
	requestValue, responseValue, errorCode string,
	secrets ...string,
) {
	t.Helper()
	for _, span := range spans {
		attrs := rpcSpanAttributes(span)
		for _, key := range []string{"lumo.request", "lumo.response"} {
			if !json.Valid([]byte(attrs[key])) {
				t.Errorf("%s missing valid %s: %q", span.Name, key, attrs[key])
			}
		}
		if requestValue != "" && !strings.Contains(attrs["lumo.request"], requestValue) {
			t.Errorf("%s lost observable request value %q: %s", span.Name, requestValue, attrs["lumo.request"])
		}
		if responseValue != "" && !strings.Contains(attrs["lumo.response"], responseValue) {
			t.Errorf("%s lost observable response value %q: %s", span.Name, responseValue, attrs["lumo.response"])
		}
		if errorCode != "" && attrs["lumo.error.code"] != errorCode {
			t.Errorf("%s error code=%q, want %q", span.Name, attrs["lumo.error.code"], errorCode)
		}
		for _, secret := range secrets {
			if strings.Contains(span.Status.Description, secret) {
				t.Errorf("%s leaked sensitive error text in status", span.Name)
			}
			for _, attr := range span.Attributes {
				if strings.Contains(attr.Value.AsString(), secret) {
					t.Errorf("%s leaked sensitive data in %s", span.Name, attr.Key)
				}
			}
		}
	}
}

func TestRPCPayloadTraceRecordsLegacyMessagesAtEveryRPCBoundary(t *testing.T) {
	exporter, _ := tracing(t)
	address := rpcTraceServer(
		t,
		Module{Name: "catalog", Handle: func(_ context.Context, request Command) (Result, error) {
			if Text(request.Data, "password") != "request-password-private" {
				t.Error("tracing changed the business request")
			}
			return Result{
				"id":           "receipt-visible",
				"total_cents":  8400,
				"access_token": "response-token-private",
				"address":      map[string]any{"detail": "response-address-private"},
			}, nil
		}},
	)
	caller := NewCaller(map[string]string{"catalog": address}, "trace-internal-token")
	t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
	result, err := caller.Call(
		context.Background(),
		"catalog",
		Command{Action: "get", UserID: "member-identity-private", Data: map[string]any{
			"sku_id": "sku-visible", "quantity": 2, "password": "request-password-private", "phone": "13912345678",
		}},
	)
	if err != nil || result["access_token"] != "response-token-private" {
		t.Fatalf("tracing changed the business result: %v %v", result, err)
	}
	assertRPCPayloadSpans(
		t,
		awaitRPCTraceSpans(t, exporter, 4),
		"sku-visible",
		"8400",
		"",
		"trace-internal-token",
		"request-password-private",
		"response-token-private",
		"member-identity-private",
		"13912345678",
		"response-address-private",
	)
}

func TestRPCPayloadTraceRecordsTypedSnapshotsWithoutLosingPrecision(t *testing.T) {
	exporter, _ := tracing(t)
	const amount int64 = 9007199254741115
	address := orderTestAddress(
		t,
		&orderRPCFixture{create: func(_ context.Context, request *mallv1.CreateOrderRequest) (*mallv1.Order, error) {
			return &mallv1.Order{
				Id:                  request.GetId(),
				PayableCents:        amount,
				QuoteSnapshotJson:   request.GetQuoteSnapshotJson(),
				AddressSnapshotJson: request.GetAddressSnapshotJson(),
			}, nil
		}},
		nil,
	)
	result, err := orderTestCaller(
		t,
		address,
	).Call(
		context.Background(),
		"order",
		Command{Action: "create", ID: "order-visible", UserID: "member-private", Role: "system", Data: map[string]any{
			"quote": map[string]any{
				"payable_cents": amount,
				"items":         []any{map[string]any{"sku_id": "sku-visible"}},
				"secret":        "snapshot-secret-private",
			},
			"address": map[string]any{
				"recipient": "recipient-private",
				"phone":     "13912345678",
				"detail":    "address-private",
			},
			"note": "freeform-private",
		}},
	)
	if err != nil || Int(result, "payable_cents") != amount {
		t.Fatalf("tracing changed typed request or result: %v %v", result, err)
	}
	assertRPCPayloadSpans(
		t,
		awaitRPCTraceSpans(t, exporter, 4),
		"9007199254741115",
		"9007199254741115",
		"",
		orderTestToken,
		"member-private",
		"snapshot-secret-private",
		"recipient-private",
		"13912345678",
		"address-private",
		"freeform-private",
	)
}

func TestRPCPayloadTraceRecordsErrorsAndRejectsSensitiveStatusText(t *testing.T) {
	for _, rejection := range []bool{false, true} {
		name := "business-error"
		if rejection {
			name = "credential-rejection"
		}
		t.Run(name, func(t *testing.T) {
			exporter, _ := tracing(t)
			address := rpcTraceServer(
				t,
				Module{Name: "catalog", Handle: func(context.Context, Command) (Result, error) {
					if rejection {
						t.Error("rejected credential reached business handler")
					}
					return nil, status.Error(codes.InvalidArgument, "business-sensitive-error-private")
				}},
			)
			token, expectedCode, count := "trace-internal-token", codes.InvalidArgument, 4
			if rejection {
				token, expectedCode, count = "wrong-internal-token-private", codes.Unauthenticated, 3
			}
			caller := NewCaller(map[string]string{"catalog": address}, token)
			t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
			_, err := caller.Call(
				context.Background(),
				"catalog",
				Command{Action: "get", ID: "order-visible", Data: map[string]any{"secret": "request-secret-private"}},
			)
			if status.Code(err) != expectedCode {
				t.Fatalf("RPC result changed: %v", err)
			}
			if !rejection && status.Convert(err).Message() != "business-sensitive-error-private" {
				t.Fatal("telemetry changed the caller's business error")
			}
			assertRPCPayloadSpans(t, awaitRPCTraceSpans(t, exporter, count), "order-visible", "", expectedCode.String(),
				token, "request-secret-private", "business-sensitive-error-private")
		})
	}
}

func TestRPCPayloadTraceRecordsCallerValidationBeforeNetworkIO(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		token   string
		targets map[string]string
		data    map[string]any
		code    codes.Code
	}{
		{"missing-credential", "", map[string]string{}, nil, codes.Unauthenticated},
		{"missing-target", "configured-token-private", map[string]string{}, nil, codes.Unavailable},
		{
			"unencodable-payload",
			"configured-token-private",
			map[string]string{"catalog": "127.0.0.1:1"},
			map[string]any{"unsupported": make(chan int)},
			codes.InvalidArgument,
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			exporter, _ := tracing(t)
			caller := NewCaller(fixture.targets, fixture.token)
			t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
			_, err := caller.Call(
				context.Background(),
				"catalog",
				Command{Action: "get", ID: "order-visible", Data: fixture.data},
			)
			if status.Code(err) != fixture.code {
				t.Fatal(err)
			}
			assertRPCPayloadSpans(
				t,
				awaitRPCTraceSpans(t, exporter, 1),
				"",
				"",
				fixture.code.String(),
				"configured-token-private",
			)
		})
	}
}

func TestRPCPayloadTraceBoundsLargeResponsesWithoutChangingThem(t *testing.T) {
	exporter, _ := tracing(t)
	address := rpcTraceServer(t, Module{Name: "catalog", Handle: func(context.Context, Command) (Result, error) {
		items := make([]any, 1000)
		for index := range items {
			items[index] = map[string]any{"id": "sku-visible", "price_cents": 12300}
		}
		return Result{"items": items, "total": len(items)}, nil
	}})
	caller := NewCaller(map[string]string{"catalog": address}, "trace-internal-token")
	t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
	result, err := caller.Call(context.Background(), "catalog", Command{Action: "list"})
	if err != nil || len(result["items"].([]any)) != 1000 {
		t.Fatalf("trace truncation changed business response: %v", err)
	}
	for _, span := range awaitRPCTraceSpans(t, exporter, 4) {
		truncated := false
		for _, attr := range span.Attributes {
			if string(attr.Key) == "lumo.response.truncated" {
				truncated = attr.Value.AsBool()
			}
		}
		if !truncated {
			t.Errorf("%s did not mark its bounded response", span.Name)
		}
	}
}
