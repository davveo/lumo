package payment_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func TestDemoSubmitCapturesOwnerPaymentOnce(t *testing.T) {
	r, dbs := setupWithDB(t)
	createOrder(t, r, "buyer-demo", time.Now().Add(time.Hour))
	p := call(t, r, "payment", "create", "buyer-demo", "user", nil)
	pid := platform.Text(p, "id")
	cmd := platform.Command{Action: "submit_demo", ID: pid, UserID: "demo", Role: "user", Data: map[string]any{"amount_cents": 1, "provider": "forged", "paid_at": "1970-01-01T00:00:00Z"}}
	paid, err := r.Call(context.Background(), "payment", cmd)
	if err != nil {
		t.Fatal(err)
	}
	if platform.Text(paid, "status") != "SUCCEEDED" || platform.Int(paid, "amount_cents") != 1000 || platform.Text(paid, "provider") != "demo" || platform.Text(paid, "receipt") != "demo-receipt-"+pid {
		t.Fatalf("wrong server capture: %v", paid)
	}
	paidAt, err := time.Parse(time.RFC3339Nano, platform.Text(paid, "paid_at"))
	if err != nil || paidAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("client supplied the capture timestamp: %v", paid)
	}
	// A late browser retry must return the original receipt even after order
	// acceptance or a close query has frozen the already captured payment gate.
	call(t, r, "order", "accept_payment", "buyer-demo", "system", map[string]any{"payment_id": pid, "amount_cents": int64(1000), "paid_at": platform.Text(paid, "paid_at")})
	call(t, r, "payment", "close", "buyer-demo", "system", nil)
	again, err := r.Call(context.Background(), "payment", cmd)
	if err != nil || platform.Text(again, "paid_at") != platform.Text(paid, "paid_at") || platform.Text(again, "receipt") != platform.Text(paid, "receipt") {
		t.Fatalf("capture retry changed the result: %v, %v", again, err)
	}
	var events []platform.Outbox
	if err := dbs["payment"].Where("event_type = ?", "PaymentSucceeded").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one success event, got %d", len(events))
	}
	var event platform.Event
	if err := json.Unmarshal([]byte(events[0].PayloadJSON), &event); err != nil {
		t.Fatal(err)
	}
	if event.AggregateID != pid || event.UserID != "demo" || platform.Text(event.Data, "order_id") != "buyer-demo" || platform.Int(event.Data, "amount_cents") != 1000 || platform.Text(event.Data, "provider") != "demo" {
		t.Fatalf("wrong success event: %#v", event)
	}
}

func TestDemoSubmitRejectsUnsafePaymentTransitions(t *testing.T) {
	type fixture func(*testing.T, router, map[string]*gorm.DB, string)
	update := func(t *testing.T, db *gorm.DB, table, id string, values map[string]any) {
		t.Helper()
		if err := db.Table(table).Where("id = ?", id).Updates(values).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, user string
		code       codes.Code
		prepare    fixture
	}{
		{name: "another buyer", user: "other", code: codes.PermissionDenied},
		{name: "unauthenticated", user: "", code: codes.Unauthenticated},
		{name: "demo disabled", user: "demo", code: codes.FailedPrecondition, prepare: func(t *testing.T, _ router, _ map[string]*gorm.DB, _ string) { t.Setenv("DEMO_MODE", "false") }},
		{name: "non demo provider", user: "demo", code: codes.FailedPrecondition, prepare: func(t *testing.T, _ router, dbs map[string]*gorm.DB, pid string) {
			update(t, dbs["payment"], "payments", pid, map[string]any{"provider": "production"})
		}},
		{name: "payment closed", user: "demo", code: codes.FailedPrecondition, prepare: func(t *testing.T, r router, _ map[string]*gorm.DB, _ string) {
			call(t, r, "payment", "close", "unsafe", "system", nil)
		}},
		{name: "cancellation started", user: "demo", code: codes.FailedPrecondition, prepare: func(t *testing.T, r router, _ map[string]*gorm.DB, _ string) {
			call(t, r, "order", "close_begin", "unsafe", "system", nil)
		}},
		{name: "deadline expired", user: "demo", code: codes.FailedPrecondition, prepare: func(t *testing.T, _ router, dbs map[string]*gorm.DB, _ string) {
			update(t, dbs["order"], "orders", "unsafe", map[string]any{"expires_at": time.Now().Add(-time.Minute)})
		}},
		{name: "amount mismatch", user: "demo", code: codes.FailedPrecondition, prepare: func(t *testing.T, _ router, dbs map[string]*gorm.DB, pid string) {
			update(t, dbs["payment"], "payments", pid, map[string]any{"amount": 900})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, dbs := setupWithDB(t)
			createOrder(t, r, "unsafe", time.Now().Add(time.Hour))
			p := call(t, r, "payment", "create", "unsafe", "user", nil)
			pid := platform.Text(p, "id")
			if tc.prepare != nil {
				tc.prepare(t, r, dbs, pid)
			}
			before := call(t, r, "payment", "get", pid, "user", nil)
			_, err := r.Call(context.Background(), "payment", platform.Command{Action: "submit_demo", ID: pid, UserID: tc.user, Role: "user"})
			if status.Code(err) != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
			after := call(t, r, "payment", "get", pid, "user", nil)
			if platform.Text(after, "status") != platform.Text(before, "status") || platform.Text(after, "receipt") != "" {
				t.Fatalf("rejected submit mutated payment: %v", after)
			}
			var count int64
			if err := dbs["payment"].Model(&platform.Outbox{}).Where("event_type = ?", "PaymentSucceeded").Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("rejected submit emitted %d success events", count)
			}
		})
	}
}

func TestExpiredOrderCannotCreatePaymentAndAdminConfirmationStaysRestricted(t *testing.T) {
	r := setup(t)
	createOrder(t, r, "expired", time.Now().Add(-time.Minute))
	_, err := r.Call(context.Background(), "payment", platform.Command{Action: "create", ID: "expired", UserID: "demo", Role: "user"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expired order accepted a new payment: %v", err)
	}
	createOrder(t, r, "admin-only", time.Now().Add(time.Hour))
	p := call(t, r, "payment", "create", "admin-only", "user", nil)
	_, err = r.Call(context.Background(), "payment", platform.Command{Action: "confirm_demo", ID: platform.Text(p, "id"), UserID: "demo", Role: "user"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("buyer gained administrator confirmation: %v", err)
	}
}
