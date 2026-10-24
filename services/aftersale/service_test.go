package aftersale

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

type fakeCaller struct {
	fn func(context.Context, string, platform.Command) (platform.Result, error)
}

func (f *fakeCaller) Call(ctx context.Context, svc string, c platform.Command) (platform.Result, error) {
	return f.fn(ctx, svc, c)
}
func setup(t *testing.T, rpc platform.Caller) (platform.Module, *gorm.DB) {
	t.Helper()
	t.Setenv("DEMO_MODE", "true")
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = platform.MigratePlatform(ctx, db); err != nil {
		t.Fatal(err)
	}
	m := New(platform.Dependencies{DB: db, RPC: rpc})
	if err = m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return m, db
}
func run(t *testing.T, m platform.Module, c platform.Command) platform.Result {
	t.Helper()
	x, e := m.Handle(context.Background(), c)
	if e != nil {
		t.Fatalf("%s: %v", c.Action, e)
	}
	return x
}
func expectCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Fatalf("got %v want %v", err, code)
	}
}

func TestReturnRefundQuantityClaimAndMoneyEvent(t *testing.T) {
	refunds := 0
	inspections := 0
	rpc := &fakeCaller{fn: func(ctx context.Context, svc string, c platform.Command) (platform.Result, error) {
		if svc == "order" {
			return platform.Result{
				"id":            c.ID,
				"user_id":       "demo",
				"status":        "SHIPPED",
				"payment_id":    "payment1",
				"payable_cents": int64(18000),
				"items": []map[string]any{
					{"sku_id": "sku-headphones", "quantity": 2, "unit_price_cents": 10000},
				},
			}, nil
		}
		if c.Role != "system" {
			t.Fatal("internal write principal required")
		}
		if svc == "refund" {
			refunds++
		}
		if svc == "fulfillment" && c.Action == "inspect_return" {
			inspections++
			if c.Data["condition"] != "damaged" {
				t.Fatal(c.Data)
			}
		}
		return platform.Result{}, nil
	}}
	m, _ := setup(t, rpc)
	ctx := context.Background()
	create := platform.Command{
		Action:         "create",
		UserID:         "demo",
		IdempotencyKey: "one",
		Data: map[string]any{
			"order_id": "o1",
			"type":     "return",
			"reason":   "quality issue",
			"lines":    []map[string]any{{"sku_id": "sku-headphones", "quantity": 1}},
		},
	}
	row := run(t, m, create)
	id := platform.Text(row, "id")
	if platform.Int(row, "amount_cents") != 9000 {
		t.Fatal(row)
	}
	if platform.Text(run(t, m, create), "id") != id {
		t.Fatal("idempotency failed")
	}
	extra := create
	extra.IdempotencyKey = "too-many"
	extra.Data = map[string]any{
		"order_id": "o1",
		"type":     "return",
		"reason":   "another",
		"lines":    []map[string]any{{"sku_id": "sku-headphones", "quantity": 2}},
	}
	_, err := m.Handle(ctx, extra)
	expectCode(t, err, codes.FailedPrecondition)
	_, err = m.Handle(ctx, platform.Command{Action: "get", ID: id, UserID: "other"})
	expectCode(t, err, codes.PermissionDenied)
	_, err = m.Handle(ctx, platform.Command{Action: "approve", ID: id, UserID: "demo"})
	expectCode(t, err, codes.PermissionDenied)
	_, err = m.Handle(ctx, platform.Command{Action: "refund", ID: id, UserID: "demo"})
	expectCode(t, err, codes.FailedPrecondition)
	if refunds != 0 {
		t.Fatal("refund before return inspection")
	}
	run(t, m, platform.Command{Action: "approve", ID: id, UserID: "admin", Role: "admin"})
	run(
		t,
		m,
		platform.Command{Action: "return", ID: id, UserID: "demo", Data: map[string]any{"tracking": "RETURN-DEMO"}},
	)
	inspect := platform.Command{
		Action: "inspect",
		ID:     id,
		UserID: "admin",
		Role:   "admin",
		Data:   map[string]any{"condition": "damaged"},
	}
	run(t, m, inspect)
	run(t, m, inspect)
	if inspections != 1 {
		t.Fatal("duplicate inventory receipt", inspections)
	}
	row = run(t, m, platform.Command{Action: "refund", ID: id, UserID: "demo"})
	if row["status"] != "REFUNDING" || refunds != 1 {
		t.Fatal(row, refunds)
	}
	e := platform.Event{
		Type:        "RefundSucceeded",
		UserID:      "demo",
		AggregateID: platform.Text(row, "refund_id"),
		Data:        map[string]any{"aftersale_id": id, "refund_id": platform.Text(row, "refund_id")},
	}
	if err = m.OnEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err = m.OnEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	row = run(t, m, platform.Command{Action: "get", ID: id, UserID: "demo"})
	if row["status"] != "COMPLETED" {
		t.Fatal(row)
	}
	run(t, m, platform.Command{Action: "refund", ID: id, UserID: "demo"})
	if refunds != 1 {
		t.Fatal("refund repeated")
	}
}
func TestExchangeCreatesSeparateFulfillmentAndWaitsForDelivery(t *testing.T) {
	calls := []string{}
	rpc := &fakeCaller{fn: func(ctx context.Context, svc string, c platform.Command) (platform.Result, error) {
		if svc == "order" {
			return platform.Result{
				"id":            "o2",
				"user_id":       "demo",
				"status":        "COMPLETED",
				"payment_id":    "p2",
				"payable_cents": int64(10000),
				"items":         []map[string]any{{"sku_id": "sku-watch", "quantity": 1, "unit_price_cents": 10000}},
			}, nil
		}
		calls = append(calls, svc+"."+c.Action)
		return platform.Result{}, nil
	}}
	m, _ := setup(t, rpc)
	row := run(
		t,
		m,
		platform.Command{
			Action:         "create",
			UserID:         "demo",
			IdempotencyKey: "exchange",
			Data:           map[string]any{"order_id": "o2", "type": "exchange", "reason": "replace defective unit"},
		},
	)
	id := platform.Text(row, "id")
	run(t, m, platform.Command{Action: "approve", ID: id, UserID: "admin", Role: "admin"})
	run(t, m, platform.Command{Action: "return", ID: id, UserID: "demo", Data: map[string]any{"tracking": "R2"}})
	run(
		t,
		m,
		platform.Command{
			Action: "inspect",
			ID:     id,
			UserID: "admin",
			Role:   "admin",
			Data:   map[string]any{"condition": "unsellable"},
		},
	)
	row = run(t, m, platform.Command{Action: "exchange", ID: id, UserID: "demo"})
	if row["status"] != "EXCHANGING" || platform.Int(row, "amount_cents") != 0 {
		t.Fatal(row)
	}
	if err := m.OnEvent(
		context.Background(),
		platform.Event{
			Type: "DeliveryConfirmed",
			Data: map[string]any{"kind": "EXCHANGE", "fulfillment_id": platform.Text(row, "replacement_id")},
		},
	); err != nil {
		t.Fatal(err)
	}
	row = run(t, m, platform.Command{Action: "get", ID: id, UserID: "demo"})
	if row["status"] != "COMPLETED" {
		t.Fatal(row)
	}
	for _, call := range calls {
		if call == "refund.create" {
			t.Fatal("exchange must not create a money refund")
		}
	}
}

func TestPartialUnshippedRefundIsRejectedWithoutSideEffects(t *testing.T) {
	writes := 0
	rpc := &fakeCaller{fn: func(ctx context.Context, svc string, c platform.Command) (platform.Result, error) {
		if svc == "order" {
			return platform.Result{
				"id":            "partial-order",
				"user_id":       "demo",
				"status":        "READY_TO_FULFILL",
				"payment_id":    "partial-payment",
				"payable_cents": int64(20000),
				"items":         []map[string]any{{"sku_id": "sku-phone", "quantity": 2, "unit_price_cents": 10000}},
			}, nil
		}
		writes++
		return platform.Result{}, nil
	}}
	m, db := setup(t, rpc)
	_, err := m.Handle(
		context.Background(),
		platform.Command{
			Action:         "create",
			UserID:         "demo",
			IdempotencyKey: "partial-refund",
			Data: map[string]any{
				"order_id": "partial-order",
				"type":     "refund",
				"reason":   "cancel one only",
				"lines":    []map[string]any{{"sku_id": "sku-phone", "quantity": 1}},
			},
		},
	)
	expectCode(t, err, codes.FailedPrecondition)
	var count int64
	if err = db.Model(&domain.Record{}).Count(&count).Error; err != nil || count != 0 || writes != 0 {
		t.Fatal("rejected request changed aftersale/warehouse/refund state", count, writes, err)
	}
	// Existing applications accepted by earlier versions must also be stopped
	// before they change state or send an order-wide warehouse cancellation.
	legacy := domain.Record{
		ID:          "legacy-partial",
		UserID:      "demo",
		OrderID:     "partial-order",
		PaymentID:   "partial-payment",
		Type:        "refund",
		Status:      "APPROVED",
		LinesJSON:   `[{"sku_id":"sku-phone","quantity":1}]`,
		AmountCents: 10000,
	}
	if err = db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	_, err = m.Handle(context.Background(), platform.Command{Action: "refund", ID: legacy.ID, UserID: "demo"})
	expectCode(t, err, codes.FailedPrecondition)
	row := run(t, m, platform.Command{Action: "get", ID: legacy.ID, UserID: "demo"})
	if row["status"] != "APPROVED" || writes != 0 {
		t.Fatal("legacy request changed state before rejection", row, writes)
	}
	// Whole-order cancellation remains supported by the existing warehouse.
	row = run(
		t,
		m,
		platform.Command{
			Action:         "create",
			UserID:         "demo",
			IdempotencyKey: "whole-refund",
			Data:           map[string]any{"order_id": "whole-order", "type": "refund", "reason": "cancel whole order"},
		},
	)
	id := platform.Text(row, "id")
	run(t, m, platform.Command{Action: "approve", ID: id, UserID: "admin", Role: "admin"})
	row = run(t, m, platform.Command{Action: "refund", ID: id, UserID: "demo"})
	if row["status"] != "REFUNDING" || writes != 2 {
		t.Fatal("whole-order refund no longer works", row, writes)
	}
}
