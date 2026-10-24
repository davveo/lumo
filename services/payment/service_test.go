package payment_test

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	ordermod "github.com/davveo/lumo/services/order"
	paymentmod "github.com/davveo/lumo/services/payment"
	refundmod "github.com/davveo/lumo/services/refund"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

type router map[string]platform.Module

func (r router) Call(ctx context.Context, name string, c platform.Command) (platform.Result, error) {
	return r[name].Handle(ctx, c)
}
func setup(t *testing.T) router {
	t.Helper()
	r, _ := setupWithDB(t)
	return r
}
func setupWithDB(t *testing.T) (router, map[string]*gorm.DB) {
	t.Helper()
	t.Setenv("DEMO_MODE", "true")
	ctx := context.Background()
	r := router{}
	dbs := map[string]*gorm.DB{}
	for name, factory := range map[string]func(platform.Dependencies) platform.Module{"order": ordermod.New, "payment": paymentmod.New, "refund": refundmod.New} {
		db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), name+".db"))
		if err != nil {
			t.Fatal(err)
		}
		sql, _ := db.DB()
		t.Cleanup(func() { _ = sql.Close() })
		if err = platform.MigratePlatform(ctx, db); err != nil {
			t.Fatal(err)
		}
		m := factory(platform.Dependencies{DB: db, RPC: r})
		if err = m.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		r[name] = m
		dbs[name] = db
	}
	return r, dbs
}
func call(t *testing.T, r router, name, action, id, role string, data map[string]any) platform.Result {
	t.Helper()
	user := "demo"
	if role == "admin" {
		user = "admin"
	}
	v, e := r.Call(context.Background(), name, platform.Command{Action: action, ID: id, UserID: user, Role: role, Data: data})
	if e != nil {
		t.Fatalf("%s.%s: %v", name, action, e)
	}
	return v
}
func createOrder(t *testing.T, r router, id string, expiry time.Time) {
	call(t, r, "order", "create", id, "system", map[string]any{"quote": map[string]any{"payable_cents": int64(1000), "items": []any{map[string]any{"sku_id": "sku-coffee", "quantity": 1}}}, "expires_at": expiry.Format(time.RFC3339Nano)})
	call(t, r, "order", "pending", id, "system", nil)
}

func TestFreezePreventsStalePaymentCreation(t *testing.T) {
	r := setup(t)
	createOrder(t, r, "freeze", time.Now().Add(time.Hour))
	call(t, r, "payment", "close", "freeze", "system", nil)
	_, e := r.Call(context.Background(), "payment", platform.Command{Action: "create", ID: "freeze", UserID: "demo", Role: "user"})
	if status.Code(e) != codes.FailedPrecondition {
		t.Fatalf("frozen gate accepted create: %v", e)
	}
}

func TestPaymentAndRefundIdempotencyAndPendingCap(t *testing.T) {
	r := setup(t)
	createOrder(t, r, "capture", time.Now().Add(time.Hour))
	p := call(t, r, "payment", "create", "capture", "user", nil)
	pid := platform.Text(p, "id")
	p2 := call(t, r, "payment", "create", "capture", "user", nil)
	if platform.Text(p2, "id") != pid {
		t.Fatal("duplicate pay created a second attempt")
	}
	call(t, r, "payment", "confirm_demo", pid, "admin", nil)
	call(t, r, "payment", "confirm_demo", pid, "admin", nil)
	base := func(amount int64) map[string]any {
		return map[string]any{"order_id": "capture", "payment_id": pid, "amount_cents": amount, "reason": "approved test return"}
	}
	call(t, r, "refund", "create", "refund-a", "system", base(600))
	call(t, r, "refund", "create", "refund-a", "system", base(600))
	_, e := r.Call(context.Background(), "refund", platform.Command{Action: "create", ID: "refund-b", UserID: "demo", Role: "system", Data: base(500)})
	if status.Code(e) != codes.FailedPrecondition {
		t.Fatalf("pending refunds not counted in cap: %v", e)
	}
	_, e = r.Call(context.Background(), "refund", platform.Command{Action: "create", ID: "refund-a", UserID: "demo", Role: "system", Data: base(700)})
	if status.Code(e) != codes.AlreadyExists {
		t.Fatalf("different amount accepted under existing refund key: %v", e)
	}
	call(t, r, "refund", "create", "refund-c", "system", base(400))
	call(t, r, "refund", "confirm_demo", "refund-a", "admin", nil)
	last := call(t, r, "refund", "confirm_demo", "refund-c", "admin", nil)
	if platform.Int(last, "refunded_total_cents") != 1000 {
		t.Fatalf("wrong cumulative refund: %v", last)
	}
	call(t, r, "refund", "confirm_demo", "refund-c", "admin", nil)
	_, e = r.Call(context.Background(), "refund", platform.Command{Action: "create", ID: "refund-d", UserID: "demo", Role: "system", Data: base(1)})
	if status.Code(e) != codes.FailedPrecondition {
		t.Fatalf("capture overrefunded: %v", e)
	}
}

func TestOrderRejectsLatePaymentAndDoesNotReviveClosed(t *testing.T) {
	r := setup(t)
	createOrder(t, r, "late", time.Now().Add(-time.Minute))
	v := call(t, r, "order", "accept_payment", "late", "system", map[string]any{"payment_id": "late-payment", "amount_cents": int64(1000), "paid_at": time.Now().UTC().Format(time.RFC3339Nano)})
	if accepted, _ := v["accepted"].(bool); accepted || platform.Text(v, "status") != "CLOSED" {
		t.Fatalf("late payment revived order: %v", v)
	}
	o := call(t, r, "order", "get", "late", "user", nil)
	if platform.Text(o, "payment_id") != "" || platform.Text(o, "status") != "CLOSED" {
		t.Fatalf("late payment bound as valid: %v", o)
	}
}

func TestOwnerAndInternalWritePermissions(t *testing.T) {
	r := setup(t)
	createOrder(t, r, "private", time.Now().Add(time.Hour))
	_, e := r.Call(context.Background(), "order", platform.Command{Action: "get", ID: "private", UserID: "other", Role: "user"})
	if status.Code(e) != codes.PermissionDenied {
		t.Fatalf("cross-member order read allowed: %v", e)
	}
	_, e = r.Call(context.Background(), "order", platform.Command{Action: "ready", ID: "private", UserID: "demo", Role: "user"})
	if status.Code(e) != codes.PermissionDenied {
		t.Fatalf("member could force dispatch: %v", e)
	}
}
