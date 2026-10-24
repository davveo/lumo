package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"github.com/davveo/lumo/internal/orderwire"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
)

const orderTestToken = "order-test-internal-token"

type orderRPCFixture struct {
	mallv1.UnimplementedOrderServiceServer
	list   func(context.Context, *mallv1.ListOrdersRequest) (*mallv1.ListOrdersResponse, error)
	get    func(context.Context, *mallv1.OrderRequest) (*mallv1.Order, error)
	create func(context.Context, *mallv1.CreateOrderRequest) (*mallv1.Order, error)
	accept func(context.Context, *mallv1.AcceptPaymentRequest) (*mallv1.AcceptPaymentResponse, error)
	refund func(context.Context, *mallv1.RecordRefundRequest) (*mallv1.Order, error)
}

func (s *orderRPCFixture) ListOrders(
	ctx context.Context,
	request *mallv1.ListOrdersRequest,
) (*mallv1.ListOrdersResponse, error) {
	if s.list != nil {
		return s.list(ctx, request)
	}
	return s.UnimplementedOrderServiceServer.ListOrders(ctx, request)
}

func (s *orderRPCFixture) GetOrder(ctx context.Context, request *mallv1.OrderRequest) (*mallv1.Order, error) {
	if s.get != nil {
		return s.get(ctx, request)
	}
	return s.UnimplementedOrderServiceServer.GetOrder(ctx, request)
}

func (s *orderRPCFixture) CreateOrder(ctx context.Context, request *mallv1.CreateOrderRequest) (*mallv1.Order, error) {
	if s.create != nil {
		return s.create(ctx, request)
	}
	return s.UnimplementedOrderServiceServer.CreateOrder(ctx, request)
}

func (s *orderRPCFixture) AcceptPayment(
	ctx context.Context,
	request *mallv1.AcceptPaymentRequest,
) (*mallv1.AcceptPaymentResponse, error) {
	if s.accept != nil {
		return s.accept(ctx, request)
	}
	return s.UnimplementedOrderServiceServer.AcceptPayment(ctx, request)
}

func (s *orderRPCFixture) RecordRefund(
	ctx context.Context,
	request *mallv1.RecordRefundRequest,
) (*mallv1.Order, error) {
	if s.refund != nil {
		return s.refund(ctx, request)
	}
	return s.UnimplementedOrderServiceServer.RecordRefund(ctx, request)
}

func orderTestAddress(
	t *testing.T,
	typed *orderRPCFixture,
	legacy func(context.Context, Command) (Result, error),
) string {
	t.Helper()
	module := Module{Name: "order", Handle: legacy}
	if typed != nil {
		module.RegisterRPC = func(registrar grpc.ServiceRegistrar) {
			mallv1.RegisterOrderServiceServer(registrar, typed)
		}
		module.RPCActions = orderwire.Actions()
	}
	server, err := NewRPCServer(orderTestToken, module)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	return listener.Addr().String()
}

func orderTestCaller(t *testing.T, address string) Caller {
	t.Helper()
	caller := NewCaller(map[string]string{"order": address}, orderTestToken)
	t.Cleanup(func() { _ = caller.(*rpcCaller).Close() })
	return caller
}

func TestOrderCallerUsesExplicitRPCAndPreservesLegacyJSONShape(t *testing.T) {
	const amount int64 = 9007199254741115
	stamp := time.Date(2026, 10, 24, 3, 4, 5, 123456789, time.UTC)
	requests := make(chan *mallv1.OrderRequest, 1)
	var legacyCalls atomic.Int32
	address := orderTestAddress(
		t,
		&orderRPCFixture{get: func(_ context.Context, request *mallv1.OrderRequest) (*mallv1.Order, error) {
			requests <- request
			return &mallv1.Order{Id: request.GetId(), UserId: "member-a", Status: "PAID_CONFIRMING", Version: 7,
				PayableCents: amount, RefundedTotalCents: amount - 1, PaymentId: "payment-a", ResourcesCommitted: true,
				ExpiresAt: timestamppb.New(stamp), CreatedAt: timestamppb.New(stamp), UpdatedAt: timestamppb.New(stamp),
				QuoteSnapshotJson: []byte(
					`{"payable_cents":9007199254741115,"items":[{"sku_id":"sku-a","price_cents":9007199254741115}],"coupon_id":"coupon-a","points":23}`,
				),
				AddressSnapshotJson: []byte(`{"recipient":"示例收件人","province":"广东省"}`)}, nil
		}},
		func(context.Context, Command) (Result, error) {
			legacyCalls.Add(1)
			return nil, status.Error(codes.Internal, "legacy Execute should not be used")
		},
	)
	result, err := orderTestCaller(
		t,
		address,
	).Call(context.Background(), "order", Command{Action: "get", ID: "order-a", UserID: "member-a", Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if request.GetId() != "order-a" || request.GetActor().GetUserId() != "member-a" ||
		request.GetActor().GetRole() != "user" ||
		legacyCalls.Load() != 0 {
		t.Fatalf("explicit method lost identity or called Execute: request=%v legacy=%d", request, legacyCalls.Load())
	}
	if Int(result, "payable_cents") != amount || Int(result, "refunded_total_cents") != amount-1 ||
		result["payable_cents"] != json.Number("9007199254741115") {
		t.Fatalf("int64 money lost precision: %v", result)
	}
	quote, ok := result["quote"].(map[string]any)
	if !ok || Int(quote, "payable_cents") != amount || result["coupon_id"] != "coupon-a" ||
		Int(result, "points") != 23 ||
		result["reservation_id"] != "order-a" {
		t.Fatalf("public quote shape changed: %v", result)
	}
	items, ok := result["items"].([]any)
	if !ok || len(items) != 1 || Int(items[0].(map[string]any), "price_cents") != amount {
		t.Fatalf("nested snapshot numbers changed: %v", result["items"])
	}
	if address, ok := result["address"].(map[string]any); !ok || address["recipient"] != "示例收件人" {
		t.Fatalf("address snapshot changed: %v", result["address"])
	}
	if result["expires_at"] != stamp.Format(time.RFC3339Nano) || result["resources_committed"] != true {
		t.Fatalf("typed fields changed on the JSON adapter: %v", result)
	}
	if _, exists := result["note"]; exists {
		t.Fatal("empty optional note should remain omitted")
	}
}

func TestOrderCallerListPreservesDecodedJSONArrayCompatibility(t *testing.T) {
	for _, count := range []int{0, 2} {
		t.Run(fmt.Sprintf("orders-%d", count), func(t *testing.T) {
			requests := make(chan *mallv1.ListOrdersRequest, 1)
			var legacyCalls atomic.Int32
			address := orderTestAddress(
				t,
				&orderRPCFixture{
					list: func(
						_ context.Context,
						request *mallv1.ListOrdersRequest,
					) (
						*mallv1.ListOrdersResponse,
						error,
					) {
						requests <- request
						response := &mallv1.ListOrdersResponse{}
						for index := 0; index < count; index++ {
							response.Items = append(
								response.Items,
								&mallv1.Order{
									Id:           fmt.Sprintf("order-%d", index),
									UserId:       "member-a",
									PayableCents: 9007199254741115,
								},
							)
						}
						return response, nil
					},
				},
				func(context.Context, Command) (Result, error) { legacyCalls.Add(1); return nil, nil },
			)
			result, err := orderTestCaller(
				t,
				address,
			).Call(context.Background(), "order", Command{Action: "list", UserID: "member-a", Role: "user"})
			if err != nil {
				t.Fatal(err)
			}
			items, ok := result["items"].([]any)
			if !ok || items == nil || len(items) != count || legacyCalls.Load() != 0 {
				t.Fatalf(
					"explicit list changed Caller JSON array compatibility: items=%#v legacy=%d",
					result["items"],
					legacyCalls.Load(),
				)
			}
			request := <-requests
			if request.GetActor().GetUserId() != "member-a" || request.GetActor().GetRole() != "user" {
				t.Fatalf("list identity changed: %v", request)
			}
			if count > 0 {
				first, ok := items[0].(map[string]any)
				if !ok || first["id"] != "order-0" || first["payable_cents"] != json.Number("9007199254741115") {
					t.Fatalf("list element or exact int64 representation changed: %#v", items[0])
				}
			}
		})
	}
}

func TestOrderCallerSendsTypedCreateSnapshotsAndPaymentAmounts(t *testing.T) {
	const amount int64 = 9007199254741115
	stamp := time.Date(2026, 10, 24, 4, 5, 6, 987654321, time.UTC)
	creates := make(chan *mallv1.CreateOrderRequest, 1)
	payments := make(chan *mallv1.AcceptPaymentRequest, 1)
	refunds := make(chan *mallv1.RecordRefundRequest, 1)
	address := orderTestAddress(t, &orderRPCFixture{
		create: func(_ context.Context, request *mallv1.CreateOrderRequest) (*mallv1.Order, error) {
			creates <- request
			return &mallv1.Order{
				Id:                  request.GetId(),
				UserId:              request.GetActor().GetUserId(),
				PayableCents:        amount,
				QuoteSnapshotJson:   request.GetQuoteSnapshotJson(),
				AddressSnapshotJson: request.GetAddressSnapshotJson(),
			}, nil
		},
		accept: func(_ context.Context, request *mallv1.AcceptPaymentRequest) (*mallv1.AcceptPaymentResponse, error) {
			payments <- request
			return &mallv1.AcceptPaymentResponse{
				Accepted:  true,
				OrderId:   request.GetId(),
				PaymentId: request.GetPaymentId(),
				Status:    "PAID_CONFIRMING",
			}, nil
		},
		refund: func(_ context.Context, request *mallv1.RecordRefundRequest) (*mallv1.Order, error) {
			refunds <- request
			return &mallv1.Order{Id: request.GetId()}, nil
		},
	}, nil)
	caller := orderTestCaller(t, address)
	quote := map[string]any{
		"payable_cents": json.Number("9007199254741115"),
		"items":         []any{map[string]any{"sku_id": "sku-a"}},
		"group_id":      "group-a",
	}
	shipping := map[string]any{"recipient": "示例收件人"}
	_, err := caller.Call(
		context.Background(),
		"order",
		Command{Action: "create", ID: "order-a", UserID: "member-a", Role: "system", Data: map[string]any{
			"quote":      quote,
			"address":    shipping,
			"expires_at": stamp.Format(time.RFC3339Nano),
			"note":       "示例备注",
			"source":     "cart",
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	created := <-creates
	wantQuote, _ := json.Marshal(quote)
	wantAddress, _ := json.Marshal(shipping)
	if !bytes.Equal(created.GetQuoteSnapshotJson(), wantQuote) ||
		!bytes.Equal(created.GetAddressSnapshotJson(), wantAddress) ||
		!created.GetExpiresAt().AsTime().Equal(stamp) ||
		created.GetNote() != "示例备注" ||
		created.GetSource() != "cart" {
		t.Fatalf("create request did not preserve immutable documents and metadata: %v", created)
	}
	result, err := caller.Call(
		context.Background(),
		"order",
		Command{Action: "accept_payment", ID: "order-a", UserID: "member-a", Role: "system", Data: map[string]any{
			"payment_id":   "payment-a",
			"amount_cents": json.Number("9007199254741115"),
			"paid_at":      stamp.Format(time.RFC3339Nano),
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	paid := <-payments
	if paid.GetAmountCents() != amount || !paid.GetPaidAt().AsTime().Equal(stamp) ||
		paid.GetPaymentId() != "payment-a" ||
		paid.GetActor().GetRole() != "system" {
		t.Fatalf("payment request changed: %v", paid)
	}
	if result["accepted"] != true || result["payment_id"] != "payment-a" || result["order_id"] != "order-a" ||
		len(result) != 4 {
		t.Fatalf("payment compatibility response changed: %v", result)
	}
	_, err = caller.Call(
		context.Background(),
		"order",
		Command{
			Action: "refunded",
			ID:     "order-a",
			UserID: "member-a",
			Role:   "system",
			Data:   map[string]any{"refund_id": "refund-a", "amount_cents": amount},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if refunded := <-refunds; refunded.GetAmountCents() != amount || refunded.GetRefundId() != "refund-a" {
		t.Fatalf("refund receipt changed: %v", refunded)
	}
}

func TestOrderCallerFallsBackOnlyWhenAnExplicitRPCIsMissing(t *testing.T) {
	for _, test := range []struct {
		name  string
		typed *orderRPCFixture
	}{{"legacy-only", nil}, {"unimplemented-method", &orderRPCFixture{}}} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			address := orderTestAddress(t, test.typed, func(_ context.Context, command Command) (Result, error) {
				calls.Add(1)
				return Result{"id": command.ID, "legacy": true, "user_id": command.UserID}, nil
			})
			result, err := orderTestCaller(
				t,
				address,
			).Call(
				context.Background(),
				"order",
				Command{Action: "get", ID: "order-a", UserID: "member-a", Role: "user"},
			)
			if err != nil || calls.Load() != 1 || result["legacy"] != true || result["user_id"] != "member-a" {
				t.Fatalf("rolling-upgrade fallback failed: result=%v error=%v calls=%d", result, err, calls.Load())
			}
		})
	}
}

func TestOrderCallerDoesNotReplayBusinessFailuresThroughExecute(t *testing.T) {
	for _, code := range []codes.Code{
		codes.PermissionDenied,
		codes.Unavailable,
		codes.Unimplemented,
		codes.FailedPrecondition,
	} {
		t.Run(code.String(), func(t *testing.T) {
			var typedCalls, legacyCalls atomic.Int32
			address := orderTestAddress(
				t,
				&orderRPCFixture{get: func(context.Context, *mallv1.OrderRequest) (*mallv1.Order, error) {
					typedCalls.Add(1)
					return nil, status.Error(code, "order business policy rejected this operation")
				}},
				func(context.Context, Command) (Result, error) {
					legacyCalls.Add(1)
					return Result{"incorrect_replay": true}, nil
				},
			)
			_, err := orderTestCaller(
				t,
				address,
			).Call(context.Background(), "order", Command{Action: "get", UserID: "member-a"})
			if status.Code(err) != code || typedCalls.Load() != 1 || legacyCalls.Load() != 0 {
				t.Fatalf(
					"business failure replayed: err=%v typed=%d legacy=%d",
					err,
					typedCalls.Load(),
					legacyCalls.Load(),
				)
			}
		})
	}
}

func TestTypedOrderRPCAppliesSharedErrorNormalization(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code codes.Code
	}{{"not-found", fmt.Errorf("wrapped repository error: %w", gorm.ErrRecordNotFound), codes.NotFound},
		{"duplicate", gorm.ErrDuplicatedKey, codes.AlreadyExists},
		{"internal", errors.New("private database details must not reach the RPC client"), codes.Internal}} {
		t.Run(test.name, func(t *testing.T) {
			address := orderTestAddress(
				t,
				&orderRPCFixture{
					get: func(context.Context, *mallv1.OrderRequest) (*mallv1.Order, error) { return nil, test.err },
				},
				nil,
			)
			_, err := orderTestCaller(
				t,
				address,
			).Call(context.Background(), "order", Command{Action: "get", UserID: "member-a"})
			if status.Code(err) != test.code || strings.Contains(status.Convert(err).Message(), "private database") {
				t.Fatalf("typed RPC bypassed shared error normalization: %v", err)
			}
		})
	}
}

func TestTypedOrderRPCRequiresInternalCredentialAndKeepsHealthPublic(t *testing.T) {
	var called atomic.Int32
	address := orderTestAddress(
		t,
		&orderRPCFixture{get: func(_ context.Context, request *mallv1.OrderRequest) (*mallv1.Order, error) {
			called.Add(1)
			return &mallv1.Order{Id: request.GetId()}, nil
		}},
		nil,
	)
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := mallv1.NewOrderServiceClient(connection)
	for _, credentials := range [][]string{nil, {"wrong-token"}, {orderTestToken, orderTestToken}} {
		ctx := context.Background()
		for _, credential := range credentials {
			ctx = metadata.AppendToOutgoingContext(ctx, rpcTokenHeader, credential)
		}
		if _, err = client.GetOrder(ctx, &mallv1.OrderRequest{Id: "order-a"}); status.Code(
			err,
		) != codes.Unauthenticated {
			t.Fatalf("unexpected authentication result: %v", err)
		}
	}
	if called.Load() != 0 {
		t.Fatalf("unauthenticated typed requests reached the handler: %d", called.Load())
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), rpcTokenHeader, orderTestToken)
	if _, err = client.GetOrder(ctx, &mallv1.OrderRequest{Id: "order-a"}); err != nil || called.Load() != 1 {
		t.Fatalf("valid internal credential was rejected: %v", err)
	}
	health, err := healthv1.NewHealthClient(connection).
		Check(context.Background(), &healthv1.HealthCheckRequest{Service: "mall.v1.OrderService"})
	if err != nil || health.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("public typed-service health check failed: %v %v", health, err)
	}
}

func TestTypedOrderRPCTraceNamesTheActualProtobufService(t *testing.T) {
	exporter, _ := tracing(t)
	address := orderTestAddress(
		t,
		&orderRPCFixture{get: func(_ context.Context, request *mallv1.OrderRequest) (*mallv1.Order, error) {
			return &mallv1.Order{Id: request.GetId()}, nil
		}},
		nil,
	)
	ctx, root := Start(context.Background(), "http.test.typed-order")
	_, err := orderTestCaller(
		t,
		address,
	).Call(
		ctx,
		"order",
		Command{
			Action: "get",
			ID:     "private-order-id",
			UserID: "member-a",
			Data:   map[string]any{"secret": "private-order-body"},
		},
	)
	root.End()
	if err != nil {
		t.Fatal(err)
	}
	clients, servers, business := 0, 0, 0
	for _, span := range awaitRPCTraceSpans(t, exporter, 5) {
		if span.SpanContext.TraceID() != root.SpanContext().TraceID() {
			t.Fatalf("typed RPC split the trace at %s", span.Name)
		}
		if span.Name == "http.test.typed-order" {
			continue
		}
		attrs := rpcSpanAttributes(span)
		if attrs["lumo.target_service"] != "order" || strings.Contains(span.Name, "DomainService/Execute") {
			t.Errorf("typed target lost its real service on %s: %v", span.Name, attrs)
		}
		for _, value := range attrs {
			if strings.Contains(value, orderTestToken) || strings.Contains(value, "private-order-body") ||
				strings.Contains(value, "member-a") {
				t.Errorf("request data leaked in span %s", span.Name)
			}
		}
		if span.Name == "mall.v1.OrderService/GetOrder" {
			if attrs["rpc.service"] != "mall.v1.OrderService" {
				t.Errorf("protobuf service was replaced with a domain label: %v", attrs)
			}
			switch span.SpanKind {
			case trace.SpanKindClient:
				clients++
				if attrs["peer.service"] != "order" {
					t.Errorf("typed RPC has wrong client peer: %v", attrs)
				}
			case trace.SpanKindServer:
				servers++
				if attrs["lumo.action"] != "get" {
					t.Errorf("typed server action missing: %v", attrs)
				}
			}
		} else {
			business++
			if attrs["lumo.action"] != "get" {
				t.Errorf("typed business action missing on %s: %v", span.Name, attrs)
			}
		}
	}
	if clients != 1 || servers != 1 || business != 2 {
		t.Fatalf("typed trace lost layers: clients=%d servers=%d business=%d", clients, servers, business)
	}
}

func TestTypedOrderRPCHonorsParentDeadlineAndCancellation(t *testing.T) {
	for _, cancelManually := range []bool{false, true} {
		name := "deadline"
		if cancelManually {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan time.Time, 1)
			stopped := make(chan error, 1)
			var legacyCalls atomic.Int32
			address := orderTestAddress(
				t,
				&orderRPCFixture{get: func(ctx context.Context, _ *mallv1.OrderRequest) (*mallv1.Order, error) {
					deadline, _ := ctx.Deadline()
					entered <- deadline
					<-ctx.Done()
					stopped <- ctx.Err()
					return nil, status.FromContextError(ctx.Err()).Err()
				}},
				func(context.Context, Command) (Result, error) { legacyCalls.Add(1); return nil, nil },
			)
			timeout := 500 * time.Millisecond
			if cancelManually {
				timeout = 3 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			wantDeadline, _ := ctx.Deadline()
			caller := orderTestCaller(t, address)
			result := make(chan error, 1)
			go func() {
				_, err := caller.Call(ctx, "order", Command{Action: "get", UserID: "member-a"})
				result <- err
			}()
			select {
			case gotDeadline := <-entered:
				if gotDeadline.IsZero() || gotDeadline.After(wantDeadline.Add(5*time.Millisecond)) {
					t.Fatalf("parent deadline was expanded: got=%v parent=%v", gotDeadline, wantDeadline)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("request did not reach the explicit RPC")
			}
			wantCode, wantContext := codes.DeadlineExceeded, context.DeadlineExceeded
			if cancelManually {
				cancel()
				wantCode, wantContext = codes.Canceled, context.Canceled
			}
			if err := <-result; status.Code(err) != wantCode || legacyCalls.Load() != 0 {
				t.Fatalf("context failure was changed or replayed: err=%v legacy=%d", err, legacyCalls.Load())
			}
			select {
			case err := <-stopped:
				if !errors.Is(err, wantContext) {
					t.Fatalf("server handler did not observe parent cancellation: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("server handler survived cancellation")
			}
		})
	}
}
