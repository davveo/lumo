package logistics

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

func TestCarrierOwnershipDeliveryAndOutbox(t *testing.T) {
	m, db := setup(t, &fakeCaller{})
	c := platform.Command{
		Action: "create",
		ID:     "o1",
		UserID: "demo",
		Role:   "system",
		Data:   map[string]any{"order_id": "o1", "kind": "ORDER"},
	}
	first := run(t, m, c)
	again := run(t, m, c)
	if first["tracking_id"] != again["tracking_id"] {
		t.Fatal("duplicate waybill")
	}
	_, err := m.Handle(context.Background(), platform.Command{Action: "get", ID: "o1", UserID: "stranger"})
	expectCode(t, err, codes.PermissionDenied)
	c = platform.Command{Action: "deliver", ID: "o1", UserID: "admin", Role: "admin"}
	out := run(t, m, c)
	run(t, m, c)
	if out["status"] != "DELIVERED" {
		t.Fatal(out)
	}
	var count int64
	if err = db.WithContext(context.Background()).Model(&platform.Outbox{}).Where(
		"event_type = ?",
		"DeliveryConfirmed",
	).Count(
		&count,
	).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("delivery emitted %d times", count)
	}
}
