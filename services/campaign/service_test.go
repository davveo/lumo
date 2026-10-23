package campaign_test

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	service "github.com/davveo/lumo/services/campaign"
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

func TestActivityQuotaAndEligibility(t *testing.T) {
	_, m := setup(t, nil)
	v := must(t, m, "evaluate", "", "demo", "user", map[string]any{"subtotal_cents": 99999, "addon_sku_id": "sku-coffee"})
	if v["eligible"] != false {
		t.Fatal(v)
	}
	v = must(t, m, "evaluate", "", "demo", "user", map[string]any{"subtotal_cents": 100000, "addon_sku_id": "sku-coffee"})
	if platform.Int(v, "discount_cents") != 6000 {
		t.Fatal(v)
	}
	for i := 0; i < 20; i++ {
		must(t, m, "reserve", fmt.Sprintf("o%d", i), fmt.Sprintf("u%d", i), "system", map[string]any{"activity_id": "flash-demo", "quantity": 1})
	}
	if _, e := invoke(m, "reserve", "overflow", "another", "system", map[string]any{"activity_id": "flash-demo", "quantity": 1}); e == nil {
		t.Fatal("quota overflow")
	}
	must(t, m, "cancel", "o0", "u0", "system", nil)
	must(t, m, "reserve", "retry", "u0", "system", map[string]any{"activity_id": "flash-demo", "quantity": 1})
	must(t, m, "confirm", "retry", "u0", "system", nil)
	if _, e := invoke(m, "reserve", "same-user", "u0", "system", map[string]any{"activity_id": "flash-demo", "quantity": 1}); e == nil {
		t.Fatal("limit bypass")
	}
}
