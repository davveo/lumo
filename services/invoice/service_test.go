package invoice

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/invoice/internal/domain"
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

func TestIssuedInvoiceRetainsOriginalAndDeduplicatesRefund(t *testing.T) {
	total := int64(0)
	rpc := &fakeCaller{fn: func(ctx context.Context, svc string, c platform.Command) (platform.Result, error) {
		return platform.Result{"id": c.ID, "user_id": "demo", "status": "READY_TO_FULFILL", "payable_cents": int64(10000), "refunded_total_cents": total}, nil
	}}
	m, _ := setup(t, rpc)
	row := run(t, m, platform.Command{Action: "create", UserID: "demo", Data: map[string]any{"order_id": "o1", "title": "个人", "kind": "personal"}})
	id := platform.Text(row, "id")
	_, err := m.Handle(context.Background(), platform.Command{Action: "issue_demo", ID: id, UserID: "demo"})
	expectCode(t, err, codes.PermissionDenied)
	row = run(t, m, platform.Command{Action: "issue_demo", ID: id, UserID: "admin", Role: "admin"})
	number := platform.Text(row, "number")
	if number == "" || platform.Int(row, "issued_cents") != 10000 {
		t.Fatal(row)
	}
	total = 4000
	e := platform.Event{Type: "RefundSucceeded", UserID: "demo", Data: map[string]any{"order_id": "o1", "refund_id": "r1", "amount_cents": int64(4000), "refunded_total_cents": total}}
	if err = m.OnEvent(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if err = m.OnEvent(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	row = run(t, m, platform.Command{Action: "get", ID: id, UserID: "demo"})
	if row["status"] != "REVERSAL_PENDING" || platform.Text(row, "number") != number || platform.Int(row, "refunded_cents") != 4000 {
		t.Fatal(row)
	}
	reversals := run(t, m, platform.Command{Action: "reversals", ID: id, UserID: "demo"})
	if len(reversals["items"].([]domain.Reversal)) != 1 {
		t.Fatal(reversals)
	}
	run(t, m, platform.Command{Action: "adjust_demo", ID: id, UserID: "admin", Role: "admin"})
	_, err = m.Handle(context.Background(), platform.Command{Action: "get", ID: id, UserID: "other"})
	expectCode(t, err, codes.PermissionDenied)
}
func TestRefundBeforeInvoiceApplicationUsesCumulativeSnapshot(t *testing.T) {
	total := int64(2500)
	reads := 0
	rpc := &fakeCaller{fn: func(ctx context.Context, svc string, c platform.Command) (platform.Result, error) {
		reads++
		if reads == 1 {
			total = 0
		} else {
			total = 2500
		}
		return platform.Result{"id": c.ID, "user_id": "demo", "status": "READY_TO_FULFILL", "payable_cents": int64(10000), "refunded_total_cents": total}, nil
	}}
	m, _ := setup(t, rpc)
	row := run(t, m, platform.Command{Action: "create", UserID: "demo", Data: map[string]any{"order_id": "o2", "title": "个人"}})
	id := platform.Text(row, "id")
	if platform.Int(row, "refunded_cents") != 2500 {
		t.Fatal(row)
	}
	if err := m.OnEvent(context.Background(), platform.Event{Type: "RefundSucceeded", UserID: "demo", Data: map[string]any{"order_id": "o2", "refund_id": "r-old", "amount_cents": int64(2500), "refunded_total_cents": int64(2500)}}); err != nil {
		t.Fatal(err)
	}
	row = run(t, m, platform.Command{Action: "issue_demo", ID: id, UserID: "admin", Role: "admin"})
	if platform.Int(row, "issued_cents") != 7500 {
		t.Fatal(row)
	}
}
