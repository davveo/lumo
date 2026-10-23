package group_test

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	service "github.com/davveo/lumo/services/group"
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

func TestGroupPaidSeatsAndPerOrderEvents(t *testing.T) {
	db, m := setup(t, nil)
	must(t, m, "reserve", "o1", "demo", "system", map[string]any{"group_id": "group-demo"})
	must(t, m, "reserve", "o2", "bob", "system", map[string]any{"group_id": "group-demo"})
	v := must(t, m, "paid", "o1", "demo", "system", nil)
	if v["formed"] != false {
		t.Fatal(v)
	}
	v = must(t, m, "paid", "o2", "bob", "system", nil)
	if v["formed"] != true || platform.Text(v, "status") != "FORMED" {
		t.Fatal(v)
	}
	must(t, m, "paid", "o2", "bob", "system", nil)
	var n int64
	if e := db.WithContext(context.Background()).Model(&platform.Outbox{}).Where("event_type = ?", "GroupFormed").Count(&n).Error; e != nil {
		t.Fatal(e)
	}
	if n != 2 {
		t.Fatalf("per-order event count %d", n)
	}
	if _, e := invoke(m, "reserve", "o3", "carl", "system", map[string]any{"group_id": "group-demo"}); e == nil {
		t.Fatal("formed group accepted extra seat")
	}
}
