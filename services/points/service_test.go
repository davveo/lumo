package points_test

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	service "github.com/davveo/lumo/services/points"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

func setup(t *testing.T, caller platform.Caller) (*gorm.DB, platform.Module) {
	t.Helper()
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { sql.Close() })
	if e = platform.MigratePlatform(ctx, db); e != nil {
		t.Fatal(e)
	}
	m := service.New(platform.Dependencies{DB: db, RPC: caller})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if m.Seed != nil {
		if e = m.Seed(ctx); e != nil {
			t.Fatal(e)
		}
	}
	return db, m
}
func invoke(m platform.Module, action, id, user, role string, data map[string]any) (platform.Result, error) {
	return m.Handle(context.Background(), platform.Command{Action: action, ID: id, UserID: user, Role: role, Data: data})
}
func must(t *testing.T, m platform.Module, action, id, user, role string, data map[string]any) platform.Result {
	t.Helper()
	v, e := invoke(m, action, id, user, role, data)
	if e != nil {
		t.Fatalf("%s: %v", action, e)
	}
	return v
}

var _ = fmt.Sprintf

func TestPointsPartialRefundAndIdempotency(t *testing.T) {
	_, m := setup(t, nil)
	must(t, m, "checkin", "", "demo", "user", nil)
	must(t, m, "checkin", "", "demo", "user", nil)
	must(t, m, "reserve", "o1", "demo", "system", map[string]any{"amount": 1001})
	must(t, m, "confirm", "o1", "demo", "system", nil)
	data := map[string]any{"order_id": "o1", "payable_cents": 1000, "refunded_total_cents": 333}
	v := must(t, m, "refund", "r1", "demo", "system", data)
	if platform.Int(v, "returned") != 333 {
		t.Fatal(v)
	}
	must(t, m, "refund", "r1", "demo", "system", data)
	v = must(t, m, "get", "", "demo", "user", nil)
	if platform.Int(v, "available") != 4352 || platform.Int(v, "frozen") != 0 {
		t.Fatal(v)
	}
	v = must(t, m, "refund", "r2", "demo", "system", map[string]any{"order_id": "o1", "payable_cents": 1000, "refunded_total_cents": 1000})
	if platform.Int(v, "returned") != 668 {
		t.Fatal(v)
	}
	v = must(t, m, "get", "", "demo", "user", nil)
	if platform.Int(v, "available") != 5020 {
		t.Fatal(v)
	}
	must(t, m, "cancel", "tomb", "demo", "system", nil)
	if _, e := invoke(m, "reserve", "tomb", "demo", "system", map[string]any{"amount": 1}); e == nil {
		t.Fatal("cancel tombstone lost")
	}
}
func TestRewardSpentBeforeRefundCreatesDebt(t *testing.T) {
	_, m := setup(t, nil)
	must(t, m, "reward", "reward-order", "demo", "system", map[string]any{"amount": 500})
	must(t, m, "reserve", "spent-order", "demo", "system", map[string]any{"amount": 5500})
	must(t, m, "confirm", "spent-order", "demo", "system", nil)
	must(t, m, "refund", "refund-reward", "demo", "system", map[string]any{"order_id": "reward-order", "payable_cents": 10000, "refunded_total_cents": 10000})
	v := must(t, m, "get", "", "demo", "user", nil)
	if platform.Int(v, "available") != 0 || platform.Int(v, "debt") != 500 {
		t.Fatal(v)
	}
	must(t, m, "checkin", "", "demo", "user", nil)
	v = must(t, m, "get", "", "demo", "user", nil)
	if platform.Int(v, "debt") != 480 || platform.Int(v, "available") != 0 {
		t.Fatal(v)
	}
}
