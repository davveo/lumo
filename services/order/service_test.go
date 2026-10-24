package order_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	ordermod "github.com/davveo/lumo/services/order"
	"github.com/davveo/lumo/services/order/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type orderTestCaller struct {
	commands []platform.Command
	services []string
	err      error
}

func (r *orderTestCaller) Call(_ context.Context, service string, command platform.Command) (platform.Result, error) {
	r.services = append(r.services, service)
	r.commands = append(r.commands, command)
	return platform.Result{}, r.err
}

type orderFixture struct {
	m   platform.Module
	db  *gorm.DB
	rpc *orderTestCaller
}

func newOrderFixture(t *testing.T) *orderFixture {
	t.Helper()
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "order.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := platform.MigratePlatform(ctx, db); err != nil {
		t.Fatal(err)
	}
	rpc := &orderTestCaller{}
	m := ordermod.New(platform.Dependencies{DB: db, RPC: rpc})
	if err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return &orderFixture{m: m, db: db, rpc: rpc}
}

func (f *orderFixture) call(t *testing.T, action, id, user, role string, data map[string]any) platform.Result {
	t.Helper()
	result, err := f.m.Handle(
		context.Background(),
		platform.Command{Action: action, ID: id, UserID: user, Role: role, Data: data},
	)
	if err != nil {
		t.Fatalf("order.%s(%s): %v", action, id, err)
	}
	return result
}

func createOrderData(expiry time.Time, group string) map[string]any {
	return map[string]any{
		"quote": map[string]any{
			"id":            "quote-one",
			"payable_cents": int64(1000),
			"coupon_id":     "coupon-one",
			"points":        int64(25),
			"group_id":      group,
			"items": []any{
				map[string]any{
					"sku_id":           "coffee-blue",
					"product_id":       "coffee",
					"quantity":         int64(2),
					"unit_price_cents": int64(500),
				},
			},
		},
		"address":    map[string]any{"recipient": "示例收货人", "address": "示例路 1 号", "phone": "13800000000"},
		"expires_at": expiry.UTC().Format(time.RFC3339Nano), "source": "cart", "note": "保留快照",
	}
}

func (f *orderFixture) create(t *testing.T, id, user, group string, expiry time.Time) platform.Result {
	t.Helper()
	return f.call(t, "create", id, user, "system", createOrderData(expiry, group))
}

func (f *orderFixture) row(t *testing.T, id string) domain.Order {
	t.Helper()
	var row domain.Order
	if err := f.db.First(&row, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func (f *orderFixture) events(t *testing.T, id string) []platform.Event {
	t.Helper()
	var rows []platform.Outbox
	if err := f.db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var events []platform.Event
	for _, row := range rows {
		var event platform.Event
		if err := json.Unmarshal([]byte(row.PayloadJSON), &event); err != nil {
			t.Fatal(err)
		}
		if event.AggregateID == id {
			events = append(events, event)
		}
	}
	return events
}

func assertEventCount(t *testing.T, events []platform.Event, kind string, want int) {
	t.Helper()
	count := 0
	for _, event := range events {
		if event.Type == kind {
			count++
		}
	}
	if count != want {
		t.Fatalf("%s event count=%d, want %d: %+v", kind, count, want, events)
	}
}

func TestOrderCreationAndPendingRetriesPreserveSnapshotsAndEvents(t *testing.T) {
	f := newOrderFixture(t)
	expiry := time.Now().Add(time.Hour).UTC()
	created := f.create(t, "retry-order", "buyer", "", expiry)
	if platform.Text(created, "status") != "CREATING" || platform.Int(created, "version") != 1 ||
		platform.Text(created, "reservation_id") != "retry-order" {
		t.Fatalf("incorrect creation: %v", created)
	}
	retry := createOrderData(expiry, "")
	retry["note"] = "must not overwrite the original"
	f.call(t, "create", "retry-order", "buyer", "system", retry)
	row := f.row(t, "retry-order")
	if row.Note != "保留快照" || row.Version != 1 || row.Payable != 1000 || row.Quote == "" || row.Address == "" {
		t.Fatalf("retry changed the immutable order: %+v", row)
	}
	first := f.call(t, "pending", row.ID, "buyer", "system", nil)
	second := f.call(t, "pending", row.ID, "buyer", "system", nil)
	if platform.Int(first, "version") != 2 || platform.Int(second, "version") != 2 ||
		platform.Text(second, "status") != "PENDING_PAYMENT" {
		t.Fatalf("pending retry was not idempotent: %v / %v", first, second)
	}
	assertEventCount(t, f.events(t, row.ID), "OrderAwaitingPayment", 1)
}

func TestOrderRejectsCrossMemberAndUnauthorizedTransitions(t *testing.T) {
	f := newOrderFixture(t)
	f.create(t, "private-order", "buyer", "", time.Now().Add(time.Hour))
	f.create(t, "someone-else", "other", "", time.Now().Add(time.Hour))
	for _, action := range []string{"get", "complete"} {
		_, err := f.m.Handle(
			context.Background(),
			platform.Command{Action: action, ID: "private-order", UserID: "other", Role: "user"},
		)
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("cross-member %s: %v", action, err)
		}
	}
	for _, action := range []string{
		"create",
		"pending",
		"create_failed",
		"accept_payment",
		"ready",
		"wait_group",
		"close_begin",
		"close_complete",
		"group_formed",
		"group_failed",
		"shipped",
		"refunded",
	} {
		_, err := f.m.Handle(
			context.Background(),
			platform.Command{Action: action, ID: "private-order", UserID: "buyer", Role: "user"},
		)
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("public internal transition %s: %v", action, err)
		}
	}
	for _, action := range []string{"get", "list", "complete"} {
		_, err := f.m.Handle(context.Background(), platform.Command{Action: action, ID: "private-order"})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("anonymous %s: %v", action, err)
		}
	}
	for _, test := range []struct {
		role, user string
		total      int
	}{{"user", "buyer", 1}, {"admin", "admin", 2}, {"system", "system", 2}} {
		result := f.call(t, "list", "", test.user, test.role, nil)
		var page struct {
			Items []domain.Order `json:"items"`
		}
		if err := platform.Decode(result, &page); err != nil || len(page.Items) != test.total {
			t.Fatalf("list visibility %s: %v %v", test.role, result, err)
		}
		if test.role == "user" && page.Items[0].UserID != "buyer" {
			t.Fatal("list exposed another member")
		}
	}
	_, err := f.m.Handle(
		context.Background(),
		platform.Command{Action: "not-an-order-method", ID: "private-order", UserID: "buyer", Role: "system"},
	)
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("unknown legacy action must remain unsupported: %v", err)
	}
}

func (f *orderFixture) pay(t *testing.T, id string) {
	t.Helper()
	f.call(t, "pending", id, "buyer", "system", nil)
	for range 2 {
		result := f.call(
			t,
			"accept_payment",
			id,
			"buyer",
			"system",
			map[string]any{
				"payment_id":   "payment-" + id,
				"amount_cents": int64(1000),
				"paid_at":      time.Now().UTC().Format(time.RFC3339Nano),
			},
		)
		if result["accepted"] != true || platform.Text(result, "status") != "PAID_CONFIRMING" {
			t.Fatalf("payment did not bind: %v", result)
		}
	}
	assertEventCount(t, f.events(t, id), "OrderPaymentAccepted", 1)
}

func TestOrderReadyShipmentAndCompletionAreIdempotent(t *testing.T) {
	f := newOrderFixture(t)
	f.create(t, "ordinary", "buyer", "", time.Now().Add(time.Hour))
	f.pay(t, "ordinary")
	for _, action := range []string{"ready", "shipped", "complete"} {
		first := f.call(t, action, "ordinary", "buyer", "system", nil)
		second := f.call(t, action, "ordinary", "buyer", "system", nil)
		if platform.Int(first, "version") != platform.Int(second, "version") {
			t.Fatalf("duplicate %s changed version: %v / %v", action, first, second)
		}
	}
	row := f.row(t, "ordinary")
	if row.Status != "COMPLETED" || !row.ResourcesCommitted || row.PaymentID != "payment-ordinary" {
		t.Fatalf("wrong completed order: %+v", row)
	}
	events := f.events(t, row.ID)
	assertEventCount(t, events, "OrderReadyToFulfill", 1)
	assertEventCount(t, events, "OrderCompleted", 1)
	for _, event := range events {
		if event.Type == "OrderReadyToFulfill" &&
			(platform.Text(event.Data, "reservation_id") != row.ID || len(event.Data["items"].([]any)) != 1) {
			t.Fatalf("fulfillment event lost order items: %+v", event)
		}
	}
}

func TestOrderGroupEventsRespectResourceCommitAndFailureBoundaries(t *testing.T) {
	f := newOrderFixture(t)
	f.create(t, "group-winner", "buyer", "team-one", time.Now().Add(time.Hour))
	f.pay(t, "group-winner")
	f.call(t, "ready", "group-winner", "buyer", "system", nil)
	if row := f.row(t, "group-winner"); row.Status != "PAID_WAIT_GROUP" || !row.ResourcesCommitted {
		t.Fatalf("resources should wait for group: %+v", row)
	}
	assertEventCount(t, f.events(t, "group-winner"), "OrderReadyToFulfill", 0)
	if err := f.m.OnEvent(
		context.Background(),
		platform.Event{Type: "GroupFormed", Data: map[string]any{"order_ids": []string{"group-winner"}}},
	); err != nil {
		t.Fatal(err)
	}
	f.call(t, "ready", "group-winner", "buyer", "system", nil)
	for _, event := range []platform.Event{
		{Type: "ShipmentDispatched", Data: map[string]any{"order_id": "group-winner", "kind": "EXCHANGE"}},
		{Type: "DeliveryConfirmed", Data: map[string]any{"order_id": "group-winner", "kind": "EXCHANGE"}},
	} {
		event.UserID = "buyer"
		if err := f.m.OnEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if row := f.row(t, "group-winner"); row.Status != "READY_TO_FULFILL" {
		t.Fatalf("exchange shipment changed original order: %+v", row)
	}
	f.call(t, "shipped", "group-winner", "buyer", "system", nil)
	before := f.row(t, "group-winner")
	if _, err := f.m.Handle(
		context.Background(),
		platform.Command{Action: "group_failed", ID: before.ID, UserID: "buyer", Role: "system"},
	); status.Code(
		err,
	) != codes.FailedPrecondition {
		t.Fatalf("dispatched group order was failed: %v", err)
	}
	if after := f.row(t, before.ID); after.Status != before.Status || after.Version != before.Version ||
		after.GroupFailed {
		t.Fatalf("rejected failure partially changed the order: %+v", after)
	}
	f.create(t, "group-loser", "buyer", "team-two", time.Now().Add(time.Hour))
	f.pay(t, "group-loser")
	if err := f.m.OnEvent(
		context.Background(),
		platform.Event{Type: "GroupFailed", Data: map[string]any{"order_id": "group-loser"}},
	); err != nil {
		t.Fatal(err)
	}
	if row := f.row(t, "group-loser"); row.Status != "TERMINATED" || !row.GroupFailed {
		t.Fatalf("failed group was not terminated: %+v", row)
	}
	if _, err := f.m.Handle(
		context.Background(),
		platform.Command{Action: "ready", ID: "group-loser", UserID: "buyer", Role: "system"},
	); status.Code(
		err,
	) != codes.FailedPrecondition {
		t.Fatalf("failed group became fulfillable: %v", err)
	}
	assertEventCount(t, f.events(t, "group-loser"), "GroupOrderTerminated", 1)
}

func TestOrderPaymentCloseRaceAndDeadlineRemainAuthoritative(t *testing.T) {
	f := newOrderFixture(t)
	f.create(t, "closing", "buyer", "", time.Now().Add(time.Hour))
	f.call(t, "pending", "closing", "buyer", "system", nil)
	f.call(t, "close_begin", "closing", "buyer", "system", nil)
	accepted := f.call(
		t,
		"accept_payment",
		"closing",
		"buyer",
		"system",
		map[string]any{"payment_id": "payment-closing", "amount_cents": int64(1000)},
	)
	if accepted["accepted"] != true || platform.Text(accepted, "status") != "PAID_CONFIRMING" {
		t.Fatalf("a valid capture lost the close race: %v", accepted)
	}
	if _, err := f.m.Handle(
		context.Background(),
		platform.Command{Action: "close_complete", ID: "closing", UserID: "buyer", Role: "system"},
	); status.Code(
		err,
	) != codes.FailedPrecondition {
		t.Fatalf("accepted payment was closed: %v", err)
	}
	f.create(t, "expired", "buyer", "", time.Now().Add(-time.Minute))
	f.call(t, "pending", "expired", "buyer", "system", nil)
	for range 2 {
		result := f.call(
			t,
			"accept_payment",
			"expired",
			"buyer",
			"system",
			map[string]any{
				"payment_id":   "late-capture",
				"amount_cents": int64(1000),
				"paid_at":      time.Now().UTC().Format(time.RFC3339Nano),
			},
		)
		if result["accepted"] != false || platform.Text(result, "status") != "CLOSED" {
			t.Fatalf("late payment revived expired order: %v", result)
		}
	}
	if row := f.row(t, "expired"); row.PaymentID != "" {
		t.Fatalf("rejected capture bound to order: %+v", row)
	}
	events := f.events(t, "expired")
	assertEventCount(t, events, "OrderClosed", 1)
	assertEventCount(t, events, "PaymentRejected", 1)
}

func TestOrderRefundReceiptsDeduplicateAndRejectedAmountsRollBack(t *testing.T) {
	f := newOrderFixture(t)
	f.create(t, "refundable", "buyer", "", time.Now().Add(time.Hour))
	f.pay(t, "refundable")
	receipt := map[string]any{"refund_id": "receipt-one", "amount_cents": int64(600)}
	first := f.call(t, "refunded", "refundable", "buyer", "system", receipt)
	second := f.call(t, "refunded", "refundable", "buyer", "system", receipt)
	if platform.Int(first, "refunded_total_cents") != 600 ||
		platform.Int(second, "version") != platform.Int(first, "version") {
		t.Fatalf("duplicate receipt changed refund balance: %v / %v", first, second)
	}
	before := f.row(t, "refundable")
	_, err := f.m.Handle(
		context.Background(),
		platform.Command{
			Action: "refunded",
			ID:     before.ID,
			UserID: "buyer",
			Role:   "system",
			Data:   map[string]any{"refund_id": "too-large", "amount_cents": int64(401)},
		},
	)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("refund over capture accepted: %v", err)
	}
	if after := f.row(t, before.ID); after.Refunded != 600 || after.Version != before.Version {
		t.Fatalf("rejected refund partially updated row: %+v", after)
	}
	var count int64
	if err := f.db.Model(&domain.RefundReceipt{}).Where("id = ?", "too-large").Count(&count).Error; err != nil ||
		count != 0 {
		t.Fatalf("rejected refund left a receipt: %d %v", count, err)
	}
	f.call(
		t,
		"refunded",
		before.ID,
		"buyer",
		"system",
		map[string]any{"refund_id": "receipt-two", "amount_cents": int64(400)},
	)
	if row := f.row(t, before.ID); row.Refunded != row.Payable {
		t.Fatalf("exact capture total should be allowed: %+v", row)
	}
}

func TestOrderMutationAndOutboxFailureRollBackTogether(t *testing.T) {
	f := newOrderFixture(t)
	f.create(t, "atomic", "buyer", "", time.Now().Add(time.Hour))
	if err := f.db.Exec(
		`CREATE TRIGGER reject_pending_event BEFORE INSERT ON outboxes WHEN NEW.event_type = 'OrderAwaitingPayment' BEGIN SELECT RAISE(ABORT, 'test outbox failure'); END`,
	).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Handle(
		context.Background(),
		platform.Command{Action: "pending", ID: "atomic", UserID: "buyer", Role: "system"},
	); err == nil {
		t.Fatal("outbox failure was swallowed")
	}
	if row := f.row(t, "atomic"); row.Status != "CREATING" || row.Version != 1 {
		t.Fatalf("outbox failure left an order mutation: %+v", row)
	}
	if len(f.events(t, "atomic")) != 0 {
		t.Fatal("failed transaction left an event")
	}
	if err := f.db.Exec("DROP TRIGGER reject_pending_event").Error; err != nil {
		t.Fatal(err)
	}
	f.call(t, "pending", "atomic", "buyer", "system", nil)
	assertEventCount(t, f.events(t, "atomic"), "OrderAwaitingPayment", 1)
	f.pay(t, "atomic")
	before := f.row(t, "atomic")
	if err := f.db.Exec(
		`CREATE TRIGGER reject_refund_update BEFORE UPDATE ON orders WHEN NEW.refunded > OLD.refunded BEGIN SELECT RAISE(ABORT, 'test order save failure'); END`,
	).Error; err != nil {
		t.Fatal(err)
	}
	_, err := f.m.Handle(
		context.Background(),
		platform.Command{
			Action: "refunded",
			ID:     "atomic",
			UserID: "buyer",
			Role:   "system",
			Data:   map[string]any{"refund_id": "rolled-back-receipt", "amount_cents": int64(100)},
		},
	)
	if err == nil {
		t.Fatal("failed order save was swallowed")
	}
	if row := f.row(t, "atomic"); row.Refunded != 0 || row.Version != before.Version {
		t.Fatalf("failed refund save left a partial order mutation: %+v", row)
	}
	var receipts int64
	if err := f.db.Model(&domain.RefundReceipt{}).Where("id = ?", "rolled-back-receipt").Count(
		&receipts,
	).Error; err != nil ||
		receipts != 0 {
		t.Fatalf("refund receipt escaped its failed order transaction: %d %v", receipts, err)
	}
}

func TestOrderTickDelegatesExpirationToCheckout(t *testing.T) {
	f := newOrderFixture(t)
	f.create(t, "expired-unpaid", "buyer", "", time.Now().Add(-time.Hour))
	f.call(t, "pending", "expired-unpaid", "buyer", "system", nil)
	f.create(t, "not-due", "buyer", "", time.Now().Add(time.Hour))
	f.call(t, "pending", "not-due", "buyer", "system", nil)
	if err := f.m.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.rpc.commands) != 1 || f.rpc.services[0] != "checkout" || f.rpc.commands[0].Action != "cancel" ||
		f.rpc.commands[0].ID != "expired-unpaid" ||
		f.rpc.commands[0].UserID != "buyer" ||
		f.rpc.commands[0].Role != "system" {
		t.Fatalf("expiration bypassed checkout resource release: %+v %+v", f.rpc.services, f.rpc.commands)
	}
	if row := f.row(t, "expired-unpaid"); row.Status != "PENDING_PAYMENT" {
		t.Fatalf("tick independently mutated the order: %+v", row)
	}
	f.rpc.err = status.Error(codes.FailedPrecondition, "payment won the race")
	if err := f.m.Tick(context.Background()); err != nil {
		t.Fatalf("normal close race should be retried later: %v", err)
	}
	f.rpc.err = errors.New("checkout unavailable")
	if err := f.m.Tick(context.Background()); !errors.Is(err, f.rpc.err) {
		t.Fatalf("checkout failure was swallowed: %v", err)
	}
}
