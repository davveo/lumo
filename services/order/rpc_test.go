package order_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"reflect"
	"testing"
	"time"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const orderRPCToken = "order-test-internal-credential"

type orderRPCFixture struct {
	*orderFixture
	client mallv1.OrderServiceClient
	legacy mallv1.DomainServiceClient
	ctx    context.Context
}

func newOrderRPCFixture(t *testing.T) *orderRPCFixture {
	t.Helper()
	f := newOrderFixture(t)
	server, err := platform.NewRPCServer(orderRPCToken, f.m)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient(
		"passthrough:///order-bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-lumo-service-token", orderRPCToken))
	return &orderRPCFixture{
		orderFixture: f,
		client:       mallv1.NewOrderServiceClient(conn),
		legacy:       mallv1.NewDomainServiceClient(conn),
		ctx:          ctx,
	}
}

func orderActor(user, role string) *mallv1.OrderActor {
	return &mallv1.OrderActor{UserId: user, Role: role}
}

func orderRequest(id, user, role string) *mallv1.OrderRequest {
	return &mallv1.OrderRequest{Id: id, Actor: orderActor(user, role)}
}

func rpcCreateRequest(t *testing.T, id, user, group string) *mallv1.CreateOrderRequest {
	t.Helper()
	data := createOrderData(time.Now().UTC().Add(time.Hour), group)
	quote := data["quote"].(map[string]any)
	quote["historical_large_integer"] = json.Number("9007199254740993")
	q, err := json.Marshal(quote)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(data["address"])
	if err != nil {
		t.Fatal(err)
	}
	expiry, err := time.Parse(time.RFC3339Nano, data["expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return &mallv1.CreateOrderRequest{
		Id:                  id,
		Actor:               orderActor(user, "system"),
		QuoteSnapshotJson:   q,
		AddressSnapshotJson: a,
		ExpiresAt:           timestamppb.New(expiry),
		Note:                "保留快照",
		Source:              "cart",
	}
}

func decodeRPCResult(t *testing.T, payload []byte) platform.Result {
	t.Helper()
	var result platform.Result
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func (f *orderRPCFixture) execute(t *testing.T, command platform.Command) platform.Result {
	t.Helper()
	payload, err := json.Marshal(command.Data)
	if err != nil {
		t.Fatal(err)
	}
	response, err := f.legacy.Execute(
		f.ctx,
		&mallv1.Command{
			Action:      command.Action,
			Id:          command.ID,
			UserId:      command.UserID,
			Role:        command.Role,
			PayloadJson: payload,
		},
	)
	if err != nil {
		t.Fatalf("legacy Execute(%s): %v", command.Action, err)
	}
	return decodeRPCResult(t, response.GetPayloadJson())
}

func (f *orderRPCFixture) assertLegacyOrder(t *testing.T, actual *mallv1.Order) {
	t.Helper()
	if actual == nil {
		t.Fatal("typed RPC returned no order")
	}
	legacy := f.execute(t, platform.Command{Action: "get", ID: actual.Id, UserID: actual.UserId, Role: "user"})
	for field, value := range map[string]string{
		"id":         actual.Id,
		"user_id":    actual.UserId,
		"status":     actual.Status,
		"payment_id": actual.PaymentId,
		"group_id":   actual.GroupId,
		"note":       actual.Note,
		"source":     actual.Source,
	} {
		if platform.Text(legacy, field) != value {
			t.Errorf("typed and legacy %s differ: %q / %v", field, value, legacy[field])
		}
	}
	for field, value := range map[string]int64{
		"version":              actual.Version,
		"payable_cents":        actual.PayableCents,
		"refunded_total_cents": actual.RefundedTotalCents,
	} {
		if platform.Int(legacy, field) != value {
			t.Errorf("typed and legacy %s differ: %d / %v", field, value, legacy[field])
		}
	}
	for field, value := range map[string]bool{
		"group_formed":        actual.GroupFormed,
		"group_failed":        actual.GroupFailed,
		"resources_committed": actual.ResourcesCommitted,
	} {
		if legacy[field] != value {
			t.Errorf("typed and legacy %s differ: %t / %v", field, value, legacy[field])
		}
	}
	for field, value := range map[string]*timestamppb.Timestamp{
		"expires_at": actual.ExpiresAt,
		"created_at": actual.CreatedAt,
		"updated_at": actual.UpdatedAt,
	} {
		parsed, err := time.Parse(time.RFC3339Nano, platform.Text(legacy, field))
		if err != nil || value == nil || !parsed.Equal(value.AsTime()) {
			t.Errorf("typed and legacy %s differ: %v / %v", field, value, legacy[field])
		}
	}
	quote, address := decodeRPCResult(t, actual.QuoteSnapshotJson), decodeRPCResult(t, actual.AddressSnapshotJson)
	if !reflect.DeepEqual(legacy["quote"], map[string]any(quote)) ||
		!reflect.DeepEqual(legacy["address"], map[string]any(address)) {
		t.Errorf("typed RPC changed historical snapshots: %v / %v", quote, legacy)
	}
	if !reflect.DeepEqual(legacy["items"], quote["items"]) || legacy["coupon_id"] != quote["coupon_id"] ||
		legacy["points"] != quote["points"] ||
		legacy["reservation_id"] != actual.Id {
		t.Errorf("legacy compatibility projection lost checkout fields: %v", legacy)
	}
	if quote["historical_large_integer"] != json.Number("9007199254740993") {
		t.Fatalf("snapshot integer precision lost across RPC: %v", quote)
	}
}

func TestOrderLegacyExecuteAndTypedClientsCanShareTheSameOrder(t *testing.T) {
	f := newOrderRPCFixture(t)
	data := createOrderData(time.Now().Add(time.Hour), "")
	data["quote"].(map[string]any)["historical_large_integer"] = json.Number("9007199254740993")
	created := f.execute(
		t,
		platform.Command{Action: "create", ID: "legacy-created", UserID: "buyer", Role: "system", Data: data},
	)
	typed, err := f.client.GetOrder(f.ctx, orderRequest("legacy-created", "buyer", "user"))
	if err != nil || typed.GetStatus() != "CREATING" || platform.Int(created, "version") != typed.GetVersion() {
		t.Fatalf("typed reader could not use a legacy-created order: %v %v", typed, err)
	}
	f.assertLegacyOrder(t, typed)
	pending, err := f.client.MarkPending(f.ctx, orderRequest(typed.Id, "buyer", "system"))
	if err != nil {
		t.Fatal(err)
	}
	legacy := f.execute(t, platform.Command{Action: "pending", ID: typed.Id, UserID: "buyer", Role: "system"})
	if platform.Text(legacy, "status") != "PENDING_PAYMENT" || platform.Int(legacy, "version") != pending.Version {
		t.Fatalf("legacy retry changed typed transition: %v / %v", legacy, pending)
	}
	assertEventCount(t, f.events(t, typed.Id), "OrderAwaitingPayment", 1)
}

func TestOrderTypedRPCAndLegacyPreserveLifecycleAndHistoricalSnapshots(t *testing.T) {
	f := newOrderRPCFixture(t)
	created, err := f.client.CreateOrder(f.ctx, rpcCreateRequest(t, "typed-main", "buyer", "team-rpc"))
	if err != nil {
		t.Fatal(err)
	}
	f.assertLegacyOrder(t, created)
	duplicate := rpcCreateRequest(t, "typed-main", "buyer", "different-team-must-not-replace")
	duplicate.Note = "must not replace the original"
	again, err := f.client.CreateOrder(f.ctx, duplicate)
	if err != nil || again.Version != created.Version || again.GroupId != created.GroupId ||
		again.Note != created.Note {
		t.Fatalf("typed create retry changed the existing snapshot: %v %v", again, err)
	}
	got, err := f.client.GetOrder(f.ctx, orderRequest(created.Id, "buyer", "user"))
	if err != nil {
		t.Fatal(err)
	}
	f.assertLegacyOrder(t, got)
	if _, err := f.client.CreateOrder(f.ctx, rpcCreateRequest(t, "other-owner", "other", "")); err != nil {
		t.Fatal(err)
	}
	page, err := f.client.ListOrders(f.ctx, &mallv1.ListOrdersRequest{Actor: orderActor("buyer", "user")})
	if err != nil || len(page.GetItems()) != 1 || page.Items[0].Id != created.Id {
		t.Fatalf("typed list leaked another member's orders: %v %v", page, err)
	}
	f.assertLegacyOrder(t, page.Items[0])
	request := orderRequest(created.Id, "buyer", "system")
	for range 2 {
		pending, err := f.client.MarkPending(f.ctx, request)
		if err != nil || pending.Status != "PENDING_PAYMENT" || pending.Version != 2 {
			t.Fatalf("typed pending retry changed semantics: %v %v", pending, err)
		}
	}
	payment := &mallv1.AcceptPaymentRequest{
		Actor:       request.Actor,
		Id:          request.Id,
		PaymentId:   "typed-payment",
		AmountCents: 1000,
		PaidAt:      timestamppb.Now(),
	}
	accepted, err := f.client.AcceptPayment(f.ctx, payment)
	if err != nil || !accepted.GetAccepted() || accepted.Status != "PAID_CONFIRMING" {
		t.Fatalf("typed payment was not accepted: %v %v", accepted, err)
	}
	legacyPayment := f.execute(
		t,
		platform.Command{
			Action: "accept_payment",
			ID:     request.Id,
			UserID: "buyer",
			Role:   "system",
			Data: map[string]any{
				"payment_id":   payment.PaymentId,
				"amount_cents": int64(1000),
				"paid_at":      payment.PaidAt.AsTime().Format(time.RFC3339Nano),
			},
		},
	)
	if legacyPayment["accepted"] != accepted.Accepted || platform.Text(legacyPayment, "order_id") != accepted.OrderId ||
		platform.Text(legacyPayment, "payment_id") != accepted.PaymentId ||
		platform.Text(legacyPayment, "status") != accepted.Status {
		t.Fatalf("typed and legacy payment results differ: %v / %v", accepted, legacyPayment)
	}
	for _, transition := range []struct {
		name, want string
		call       func(context.Context, *mallv1.OrderRequest, ...grpc.CallOption) (*mallv1.Order, error)
	}{
		{"wait", "PAID_WAIT_GROUP", f.client.WaitForGroup},
		{"formed", "PAID_WAIT_GROUP", f.client.MarkGroupFormed},
		{"ready", "READY_TO_FULFILL", f.client.MarkReady},
		{"shipped", "SHIPPED", f.client.MarkShipped},
		{"complete", "COMPLETED", f.client.CompleteOrder},
	} {
		result, err := transition.call(f.ctx, request)
		if err != nil || result.GetStatus() != transition.want {
			t.Fatalf("typed %s: %v %v", transition.name, result, err)
		}
		f.assertLegacyOrder(t, result)
	}
	refund := &mallv1.RecordRefundRequest{
		Actor:       request.Actor,
		Id:          request.Id,
		RefundId:    "typed-receipt",
		AmountCents: 400,
	}
	first, err := f.client.RecordRefund(f.ctx, refund)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.client.RecordRefund(f.ctx, refund)
	if err != nil || second.RefundedTotalCents != 400 || second.Version != first.Version {
		t.Fatalf("typed refund retry changed balance: %v %v", second, err)
	}
	f.assertLegacyOrder(t, second)
	assertEventCount(t, f.events(t, created.Id), "OrderAwaitingPayment", 1)
	assertEventCount(t, f.events(t, created.Id), "OrderPaymentAccepted", 1)
	assertEventCount(t, f.events(t, created.Id), "OrderReadyToFulfill", 1)
	assertEventCount(t, f.events(t, created.Id), "OrderCompleted", 1)
}

func TestOrderTypedRPCCloseCreateFailureAndGroupFailureMatchLegacy(t *testing.T) {
	f := newOrderRPCFixture(t)
	for _, id := range []string{"typed-close", "typed-create-failed", "typed-group-failed"} {
		if _, err := f.client.CreateOrder(f.ctx, rpcCreateRequest(t, id, "buyer", "")); err != nil {
			t.Fatal(err)
		}
	}
	failed, err := f.client.MarkCreateFailed(f.ctx, orderRequest("typed-create-failed", "buyer", "system"))
	if err != nil || failed.GetStatus() != "CREATE_FAILED" {
		t.Fatalf("typed create failure: %v %v", failed, err)
	}
	f.assertLegacyOrder(t, failed)
	request := orderRequest("typed-close", "buyer", "system")
	if _, err := f.client.MarkPending(f.ctx, request); err != nil {
		t.Fatal(err)
	}
	closing, err := f.client.BeginClose(f.ctx, request)
	if err != nil || closing.GetStatus() != "CLOSING" {
		t.Fatalf("typed begin close: %v %v", closing, err)
	}
	f.assertLegacyOrder(t, closing)
	closed, err := f.client.CompleteClose(f.ctx, request)
	if err != nil || closed.GetStatus() != "CLOSED" {
		t.Fatalf("typed complete close: %v %v", closed, err)
	}
	f.assertLegacyOrder(t, closed)
	if _, err := f.client.CompleteClose(f.ctx, request); err != nil {
		t.Fatalf("typed close retry: %v", err)
	}
	assertEventCount(t, f.events(t, request.Id), "OrderClosed", 1)
	group, err := f.client.MarkGroupFailed(f.ctx, orderRequest("typed-group-failed", "buyer", "system"))
	if err != nil || group.GetStatus() != "TERMINATED" || !group.GroupFailed {
		t.Fatalf("typed group failure: %v %v", group, err)
	}
	f.assertLegacyOrder(t, group)
	if _, err := f.client.MarkReady(f.ctx, orderRequest(group.Id, "buyer", "system")); status.Code(
		err,
	) != codes.FailedPrecondition {
		t.Fatalf("failed group became ready: %v", err)
	}
	assertEventCount(t, f.events(t, group.Id), "GroupOrderTerminated", 1)
}

func TestOrderAllTypedMethodsRequireInternalCredential(t *testing.T) {
	f := newOrderRPCFixture(t)
	actor := orderActor("buyer", "system")
	request := &mallv1.OrderRequest{Actor: actor, Id: "not-a-real-order"}
	methods := map[string]func(context.Context) error{
		"list": func(ctx context.Context) error {
			_, err := f.client.ListOrders(ctx, &mallv1.ListOrdersRequest{Actor: actor})
			return err
		},
		"get": func(ctx context.Context) error { _, err := f.client.GetOrder(ctx, request); return err },
		"create": func(ctx context.Context) error {
			_, err := f.client.CreateOrder(ctx, &mallv1.CreateOrderRequest{Actor: actor, Id: request.Id})
			return err
		},
		"pending": func(ctx context.Context) error { _, err := f.client.MarkPending(ctx, request); return err },
		"create_failed": func(ctx context.Context) error {
			_, err := f.client.MarkCreateFailed(
				ctx,
				request,
			)
			return err
		},
		"payment": func(ctx context.Context) error {
			_, err := f.client.AcceptPayment(ctx, &mallv1.AcceptPaymentRequest{Actor: actor, Id: request.Id})
			return err
		},
		"ready":       func(ctx context.Context) error { _, err := f.client.MarkReady(ctx, request); return err },
		"wait_group":  func(ctx context.Context) error { _, err := f.client.WaitForGroup(ctx, request); return err },
		"begin_close": func(ctx context.Context) error { _, err := f.client.BeginClose(ctx, request); return err },
		"complete_close": func(ctx context.Context) error {
			_, err := f.client.CompleteClose(
				ctx,
				request,
			)
			return err
		},
		"group_formed": func(ctx context.Context) error {
			_, err := f.client.MarkGroupFormed(
				ctx,
				request,
			)
			return err
		},
		"group_failed": func(ctx context.Context) error {
			_, err := f.client.MarkGroupFailed(
				ctx,
				request,
			)
			return err
		},
		"shipped": func(ctx context.Context) error { _, err := f.client.MarkShipped(ctx, request); return err },
		"complete": func(ctx context.Context) error {
			_, err := f.client.CompleteOrder(
				ctx,
				request,
			)
			return err
		},
		"refund": func(ctx context.Context) error {
			_, err := f.client.RecordRefund(ctx, &mallv1.RecordRefundRequest{Actor: actor, Id: request.Id})
			return err
		},
		"legacy": func(ctx context.Context) error {
			_, err := f.legacy.Execute(
				ctx,
				&mallv1.Command{Action: "get", Id: request.Id, UserId: "buyer", Role: "system"},
			)
			return err
		},
	}
	for name, call := range methods {
		for _, token := range []string{"", "wrong-credential"} {
			t.Run(name+"/"+token, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if token != "" {
					ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-lumo-service-token", token))
				}
				if err := call(ctx); status.Code(err) != codes.Unauthenticated {
					t.Fatalf("credential-less RPC reached business logic: %v", err)
				}
			})
		}
	}
}

func TestOrderTypedRPCDoesNotBypassMemberOrInternalPermissions(t *testing.T) {
	f := newOrderRPCFixture(t)
	if _, err := f.client.CreateOrder(f.ctx, rpcCreateRequest(t, "private-rpc", "buyer", "")); err != nil {
		t.Fatal(err)
	}
	for _, method := range []func(context.Context, *mallv1.OrderRequest, ...grpc.CallOption) (*mallv1.Order, error){
		f.client.GetOrder,
		f.client.CompleteOrder,
	} {
		if _, err := method(f.ctx, orderRequest("private-rpc", "other", "user")); status.Code(
			err,
		) != codes.PermissionDenied {
			t.Fatalf("typed RPC exposed another member's order: %v", err)
		}
	}
	request := orderRequest("private-rpc", "buyer", "user")
	for _, method := range []func(context.Context, *mallv1.OrderRequest, ...grpc.CallOption) (*mallv1.Order, error){
		f.client.MarkPending,
		f.client.MarkCreateFailed,
		f.client.MarkReady,
		f.client.WaitForGroup,
		f.client.BeginClose,
		f.client.CompleteClose,
		f.client.MarkGroupFormed,
		f.client.MarkGroupFailed,
		f.client.MarkShipped,
	} {
		if _, err := method(f.ctx, request); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("member invoked internal typed transition: %v", err)
		}
	}
	create := rpcCreateRequest(t, "member-created", "buyer", "")
	create.Actor.Role = "user"
	if _, err := f.client.CreateOrder(f.ctx, create); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("member invoked internal typed create: %v", err)
	}
	if _, err := f.client.AcceptPayment(
		f.ctx,
		&mallv1.AcceptPaymentRequest{
			Actor:       request.Actor,
			Id:          request.Id,
			PaymentId:   "forged-capture",
			AmountCents: 1000,
		},
	); status.Code(
		err,
	) != codes.PermissionDenied {
		t.Fatalf("member accepted its own payment: %v", err)
	}
	if _, err := f.client.RecordRefund(
		f.ctx,
		&mallv1.RecordRefundRequest{Actor: request.Actor, Id: request.Id, RefundId: "forged-refund", AmountCents: 100},
	); status.Code(
		err,
	) != codes.PermissionDenied {
		t.Fatalf("member recorded its own refund: %v", err)
	}
	if _, err := f.client.GetOrder(f.ctx, &mallv1.OrderRequest{Id: request.Id}); status.Code(
		err,
	) != codes.Unauthenticated {
		t.Fatalf("missing actor was accepted: %v", err)
	}
	if _, err := f.legacy.Execute(
		f.ctx,
		&mallv1.Command{Action: "unknown-method", Id: request.Id, UserId: "buyer", Role: "system"},
	); status.Code(
		err,
	) != codes.Unimplemented {
		t.Fatalf("unknown legacy action was accepted: %v", err)
	}
	if row := f.row(t, request.Id); row.Status != "CREATING" || row.Version != 1 || row.Refunded != 0 ||
		row.PaymentID != "" {
		t.Fatalf("denied typed methods mutated the order: %+v", row)
	}
}
