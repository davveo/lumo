package flash_test

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	service "github.com/davveo/lumo/services/flash"
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
	return m.Handle(
		context.Background(),
		platform.Command{Action: action, ID: id, UserID: user, Role: role, Data: data},
	)
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

type fakeCaller struct{ calls int }

func (f *fakeCaller) Call(ctx context.Context, target string, c platform.Command) (platform.Result, error) {
	if target == "pricing" {
		return platform.Result{
			"activity_id": "flash-demo",
			"items":       []map[string]any{{"sku_id": "sku-headphones", "quantity": 1}},
		}, nil
	}
	if target == "checkout" {
		f.calls++
		return platform.Result{"order_id": "created-order", "status": "PENDING_PAYMENT"}, nil
	}
	return nil, fmt.Errorf("unexpected peer %s", target)
}
func TestFlashQueueAndDuplicateDelivery(t *testing.T) {
	f := &fakeCaller{}
	_, m := setup(t, f)
	c := platform.Command{
		Action:         "request",
		UserID:         "demo",
		Role:           "user",
		IdempotencyKey: "click-1",
		Data:           map[string]any{"quote_id": "quote-1", "address": map[string]any{"city": "demo"}},
	}
	v, e := m.Handle(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	if platform.Text(v, "status") != "QUEUED" || f.calls != 0 {
		t.Fatal("admission claimed order success", v)
	}
	id := platform.Text(v, "id")
	if _, e = m.Handle(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	evt := platform.Event{Type: "FlashOrderRequested", AggregateID: id, UserID: "demo"}
	if e = m.OnEvent(context.Background(), evt); e != nil {
		t.Fatal(e)
	}
	if e = m.OnEvent(context.Background(), evt); e != nil {
		t.Fatal(e)
	}
	v = must(t, m, "get", id, "demo", "user", nil)
	if f.calls != 1 || platform.Text(v, "status") != "SUCCEEDED" {
		t.Fatal(v, f.calls)
	}
	if _, e = invoke(m, "get", id, "other", "user", nil); e == nil {
		t.Fatal("foreign request exposed")
	}
}
