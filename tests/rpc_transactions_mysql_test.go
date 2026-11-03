package tests_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon"
	"github.com/davveo/lumo/services/inventory"
	"github.com/davveo/lumo/services/payment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type preflightCaller struct {
	fn func(context.Context, string, platform.Command) (platform.Result, error)
}

func (p preflightCaller) Call(ctx context.Context, service string, c platform.Command) (platform.Result, error) {
	return p.fn(ctx, service, c)
}

func TestMySQLPaymentCloseDoesNotWaitForOrderRPC(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	ctx, db := adminMySQLScratch(t, "payment_preflight")
	if e := platform.MigratePlatform(ctx, db); e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	block := false
	rpc := preflightCaller{
		fn: func(callCtx context.Context, service string, c platform.Command) (platform.Result, error) {
			if service != "order" || c.Action != "get" {
				return nil, status.Error(codes.Internal, "unexpected peer")
			}
			if block {
				close(entered)
				select {
				case <-release:
				case <-callCtx.Done():
					return nil, callCtx.Err()
				}
			}
			return platform.Result{
				"id":            "order-preflight",
				"user_id":       "buyer",
				"status":        "PENDING_PAYMENT",
				"payable_cents": 1000,
				"expires_at":    time.Now().Add(time.Hour).Format(time.RFC3339Nano),
			}, nil
		},
	}
	m := payment.New(platform.Dependencies{DB: db, RPC: rpc})
	if e := m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	created, e := m.Handle(
		ctx,
		platform.Command{Action: "create", ID: "order-preflight", UserID: "buyer", Role: "user"},
	)
	if e != nil {
		t.Fatal(e)
	}
	block = true
	submitted := make(chan error, 1)
	go func() {
		_, e := m.Handle(
			ctx,
			platform.Command{Action: "submit_demo", ID: platform.Text(created, "id"), UserID: "buyer", Role: "user"},
		)
		submitted <- e
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("order preflight did not start")
	}
	// If Order.Get runs inside the payment transaction, Close blocks on its gate.
	closeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, e = m.Handle(closeCtx, platform.Command{Action: "close", ID: "order-preflight", UserID: "buyer", Role: "system"})
	unblock.Do(func() { close(release) })
	submitErr := <-submitted
	if e != nil {
		t.Fatalf("remote preflight held payment lock: %v", e)
	}
	if status.Code(submitErr) != codes.FailedPrecondition {
		t.Fatalf("capture ignored concurrent close: %v", submitErr)
	}
	var n int64
	if e = db.Model(&platform.Outbox{}).Where("event_type = ?", "PaymentSucceeded").Count(&n).Error; e != nil ||
		n != 0 {
		t.Fatalf("closed payment emitted success: %d %v", n, e)
	}
}

func TestMySQLCouponPreflightDoesNotLockTemplateAndRechecksNewRequirements(t *testing.T) {
	ctx, db := adminMySQLScratch(t, "coupon_preflight")
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	rpc := preflightCaller{
		fn: func(callCtx context.Context, service string, c platform.Command) (platform.Result, error) {
			if service != "points" || c.Action != "get" {
				return nil, status.Error(codes.Internal, "unexpected peer")
			}
			close(entered)
			select {
			case <-release:
			case <-callCtx.Done():
				return nil, callCtx.Err()
			}
			return platform.Result{"available": 100}, nil
		},
	}
	m := coupon.New(platform.Dependencies{DB: db, RPC: rpc})
	if e := m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	if e := db.Table("templates").Create(map[string]any{
		"id":         "preflight-template",
		"name":       "Preflight",
		"amount":     100,
		"minimum":    1000,
		"valid_days": 7,
		"remaining":  10,
		"active":     true,
		"min_points": 1,
		"min_growth": 0,
		"expires_at": now.Add(time.Hour),
		"created_at": now,
	}).Error; e != nil {
		t.Fatal(e)
	}
	claimed := make(chan error, 1)
	go func() {
		_, e := m.Handle(
			ctx,
			platform.Command{Action: "claim", ID: "preflight-template", UserID: "buyer", Role: "user"},
		)
		claimed <- e
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("eligibility preflight did not start")
	}
	updateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	e := db.WithContext(updateCtx).
		Table("templates").
		Where("id = ?", "preflight-template").
		Update("min_growth", 1000).
		Error
	unblock.Do(func() { close(release) })
	claimErr := <-claimed
	if e != nil {
		t.Fatalf("remote eligibility held template lock: %v", e)
	}
	if status.Code(claimErr) != codes.Aborted {
		t.Fatalf("claim bypassed changed growth requirement: %v", claimErr)
	}
	var n int64
	if e = db.Table("claims").Count(&n).Error; e != nil || n != 0 {
		t.Fatalf("rejected eligibility issued coupon: %d %v", n, e)
	}
}

func TestMySQLConcurrentRestockBatchDoesNotDuplicateStock(t *testing.T) {
	ctx, db := adminMySQLScratch(t, "restock_batch")
	m := inventory.New(platform.Dependencies{DB: db})
	if e := m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Handle(ctx, platform.Command{
		Action: "adjust",
		ID:     "batch-sku",
		UserID: "admin",
		Role:   "admin",
		Data:   map[string]any{"on_hand": 0},
	}); e != nil {
		t.Fatal(e)
	}
	cmd := platform.Command{
		Action: "restock_batch",
		UserID: "buyer",
		Role:   "system",
		Data: map[string]any{
			"items": []map[string]any{
				{"id": "receipt-a", "sku_id": "batch-sku", "quantity": 2, "condition": "sellable"},
				{"id": "receipt-b", "sku_id": "batch-sku", "quantity": 3, "condition": "sellable"},
			},
		},
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; _, e := m.Handle(ctx, cmd); errs <- e }()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if e := <-errs; e != nil {
			t.Fatal(e)
		}
	}
	got, e := m.Handle(ctx, platform.Command{Action: "get", ID: "batch-sku"})
	if e != nil || platform.Int(got, "on_hand") != 5 {
		t.Fatalf("concurrent replay double-restocked: %v %v", got, e)
	}
}
