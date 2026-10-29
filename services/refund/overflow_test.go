package refund_test

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	refundmod "github.com/davveo/lumo/services/refund"
	"github.com/davveo/lumo/services/refund/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type capturedPayment struct{}

func (capturedPayment) Call(context.Context, string, platform.Command) (platform.Result, error) {
	return platform.Result{
		"id":           "payment",
		"order_id":     "order",
		"user_id":      "buyer",
		"status":       "SUCCEEDED",
		"amount_cents": int64(1000),
	}, nil
}

func TestRefundCapRejectsOverflowWithoutChangingItsLedger(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "refund.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sql.Close() })
	if err = platform.MigratePlatform(ctx, db); err != nil {
		t.Fatal(err)
	}
	m := refundmod.New(platform.Dependencies{DB: db, RPC: capturedPayment{}})
	if err = m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	create := func(id string, amount int64) error {
		_, err := m.Handle(
			ctx,
			platform.Command{Action: "create", ID: id, UserID: "buyer", Role: "system", Data: map[string]any{
				"payment_id": "payment", "order_id": "order", "amount_cents": amount,
			}},
		)
		return err
	}
	if err = create("valid", 600); err != nil {
		t.Fatal(err)
	}
	for _, completed := range []bool{false, true} {
		if completed {
			if _, err = m.Handle(ctx, platform.Command{Action: "confirm_demo", ID: "valid", UserID: "admin", Role: "admin"}); err != nil {
				t.Fatal(err)
			}
		}
		if err = create("overflow", math.MaxInt64); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("completed=%v: refund cap overflow accepted: %v", completed, err)
		}
		var cap domain.Cap
		if err = db.First(&cap, "payment_id = ?", "payment").Error; err != nil {
			t.Fatal(err)
		}
		if cap.Paid != 1000 || cap.Reserved+cap.Completed != 600 || cap.Reserved < 0 || cap.Completed < 0 {
			t.Fatalf("rejected refund changed capture ledger: %+v", cap)
		}
		var count int64
		if err = db.Model(&domain.Refund{}).Where("id = ?", "overflow").Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rejected refund was persisted: count=%d err=%v", count, err)
		}
	}
}
