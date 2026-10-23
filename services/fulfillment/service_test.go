package fulfillment

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
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

func TestWarehouseDispatchCancellationAndReceiptIdempotency(t *testing.T) {
	calls := map[string]int{}
	fail := true
	rpc := &fakeCaller{fn: func(ctx context.Context, svc string, c platform.Command) (platform.Result, error) {
		if c.Role != "system" {
			t.Fatal("RPC must use service principal")
		}
		calls[svc+"."+c.Action]++
		if svc == "inventory" && c.Action == "dispatch" && fail {
			fail = false
			return nil, status.Error(codes.Unavailable, "retry warehouse stock confirmation")
		}
		if svc == "inventory" && c.Action == "cancel" && c.Data["warehouse_confirmed"] != true {
			t.Fatal("allocation cancellation requires warehouse acknowledgement")
		}
		if svc == "logistics" {
			return platform.Result{"tracking_id": "DEMO-TRACK"}, nil
		}
		return platform.Result{}, nil
	}}
	m, _ := setup(t, rpc)
	ctx := context.Background()
	if err := m.OnEvent(ctx, platform.Event{Type: "OrderReadyToFulfill", AggregateID: "o1", UserID: "demo", Data: map[string]any{"order_id": "o1", "reservation_id": "o1"}}); err != nil {
		t.Fatal(err)
	}
	admin := platform.Command{Action: "dispatch", ID: "o1", UserID: "admin", Role: "admin"}
	_, err := m.Handle(ctx, admin)
	expectCode(t, err, codes.Unavailable)
	out := run(t, m, admin)
	if out["status"] != "DISPATCHED" {
		t.Fatal(out)
	}
	run(t, m, admin)
	if calls["inventory.dispatch"] != 2 || calls["logistics.create"] != 1 {
		t.Fatal(calls)
	}
	_, err = m.Handle(ctx, platform.Command{Action: "get", ID: "o1", UserID: "other"})
	expectCode(t, err, codes.PermissionDenied)
	_, err = m.Handle(ctx, platform.Command{Action: "cancel", ID: "o1", UserID: "demo", Role: "system"})
	expectCode(t, err, codes.FailedPrecondition)
	if err = m.OnEvent(ctx, platform.Event{Type: "OrderReadyToFulfill", AggregateID: "o2", UserID: "demo", Data: map[string]any{"order_id": "o2"}}); err != nil {
		t.Fatal(err)
	}
	out = run(t, m, platform.Command{Action: "cancel", ID: "o2", UserID: "demo", Role: "system"})
	if out["status"] != "CANCELLED" || out["warehouse_confirmed"] != true {
		t.Fatal(out)
	}
	// Cancellation arriving before MQ readiness must leave a durable tombstone.
	run(t, m, platform.Command{Action: "cancel", ID: "o-late", UserID: "demo", Role: "system"})
	if err = m.OnEvent(ctx, platform.Event{Type: "OrderReadyToFulfill", AggregateID: "o-late", UserID: "demo", Data: map[string]any{"order_id": "o-late"}}); err != nil {
		t.Fatal(err)
	}
	late := run(t, m, platform.Command{Action: "get", ID: "o-late", UserID: "demo"})
	if late["status"] != "CANCELLED" {
		t.Fatal("late event resurrected cancelled shipment", late)
	}
	receipt := platform.Command{Action: "inspect_return", ID: "inspection1", UserID: "demo", Role: "system", Data: map[string]any{"order_id": "o1", "sku_id": "sku-phone", "quantity": 1, "condition": "damaged"}}
	run(t, m, receipt)
	run(t, m, receipt)
	if calls["inventory.restock"] != 1 {
		t.Fatal(calls)
	}
	receipt.Data["condition"] = "sellable"
	_, err = m.Handle(ctx, receipt)
	expectCode(t, err, codes.AlreadyExists)
}
