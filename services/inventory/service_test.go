package inventory_test

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	service "github.com/davveo/lumo/services/inventory"
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

func TestInventoryReservationsAndWarehouseBoundary(t *testing.T) {
	_, m := setup(t, nil)
	reserve := func(id, user string, n int64) (platform.Result, error) {
		return invoke(m, "reserve", id, user, "system", map[string]any{"order_id": id, "lines": []map[string]any{{"sku_id": "sku-phone", "quantity": n}}})
	}
	if _, e := invoke(m, "reserve", "x", "demo", "user", map[string]any{}); e == nil {
		t.Fatal("user cannot mutate stock")
	}
	must(t, m, "cancel", "tomb", "demo", "system", nil)
	if _, e := reserve("tomb", "demo", 1); e == nil {
		t.Fatal("cancel-before-reserve must remain cancelled")
	}
	if _, e := reserve("o1", "demo", 2); e != nil {
		t.Fatal(e)
	}
	if _, e := reserve("o1", "demo", 2); e != nil {
		t.Fatal(e)
	}
	must(t, m, "confirm", "o1", "demo", "system", nil)
	if _, e := invoke(m, "cancel", "o1", "demo", "system", nil); e == nil {
		t.Fatal("allocated cancellation requires warehouse acknowledgement")
	}
	must(t, m, "dispatch", "o1", "demo", "system", nil)
	must(t, m, "dispatch", "o1", "demo", "system", nil)
	v := must(t, m, "get", "sku-phone", "", "", nil)
	if platform.Int(v, "on_hand") != 48 || platform.Int(v, "allocated") != 0 {
		t.Fatal(v)
	}
	if _, e := invoke(m, "cancel", "o1", "demo", "system", map[string]any{"warehouse_confirmed": true}); e == nil {
		t.Fatal("dispatched stock cannot be cancelled")
	}
	must(t, m, "restock", "inspection1", "demo", "system", map[string]any{"sku_id": "sku-phone", "quantity": 1, "condition": "damaged"})
	v = must(t, m, "get", "sku-phone", "", "", nil)
	if platform.Int(v, "on_hand") != 48 {
		t.Fatal("damaged stock entered sellable")
	}
	must(t, m, "restock", "inspection2", "demo", "system", map[string]any{"sku_id": "sku-phone", "quantity": 1, "condition": "sellable"})
	must(t, m, "restock", "inspection2", "demo", "system", map[string]any{"sku_id": "sku-phone", "quantity": 1, "condition": "sellable"})
	v = must(t, m, "get", "sku-phone", "", "", nil)
	if platform.Int(v, "on_hand") != 49 {
		t.Fatal(v)
	}
}
func TestInventoryConcurrentReservationsNeverOversell(t *testing.T) {
	_, m := setup(t, nil)
	must(t, m, "adjust", "sku-phone", "admin", "admin", map[string]any{"on_hand": 3})
	results := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			_, e := invoke(m, "reserve", fmt.Sprintf("r%d", i), fmt.Sprintf("u%d", i), "system", map[string]any{"order_id": fmt.Sprintf("o%d", i), "lines": []map[string]any{{"sku_id": "sku-phone", "quantity": 1}}})
			results <- e == nil
		}(i)
	}
	n := 0
	for i := 0; i < 20; i++ {
		if <-results {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("successful holds=%d", n)
	}
	v := must(t, m, "get", "sku-phone", "", "", nil)
	if platform.Int(v, "available") != 0 || platform.Int(v, "reserved") != 3 {
		t.Fatal(v)
	}
}
