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
	v = must(
		t,
		m,
		"refund",
		"r2",
		"demo",
		"system",
		map[string]any{"order_id": "o1", "payable_cents": 1000, "refunded_total_cents": 1000},
	)
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
	must(
		t,
		m,
		"refund",
		"refund-reward",
		"demo",
		"system",
		map[string]any{"order_id": "reward-order", "payable_cents": 10000, "refunded_total_cents": 10000},
	)
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

func TestCompletionRewardsFollowRefundProgressRegardlessOfEventOrder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		totals   []int64
		want     int64
		spent    int64
		wantHold string
	}{
		{name: "full refund precedes completion", totals: []int64{10000}, want: 5000},
		{name: "partial refund precedes completion", totals: []int64{2500}, want: 5075},
		{
			name:     "older partial event follows full refund",
			totals:   []int64{10000, 2500},
			want:     5000,
			spent:    1000,
			wantHold: "REFUNDED",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m := setup(t, nil)
			ctx := context.Background()
			if tc.spent > 0 {
				must(t, m, "reserve", "reordered-order", "demo", "system", map[string]any{"amount": tc.spent})
				must(t, m, "confirm", "reordered-order", "demo", "system", nil)
			}
			for i, total := range tc.totals {
				event := platform.Event{
					Type:        "RefundSucceeded",
					AggregateID: fmt.Sprintf("reordered-refund-%d", i),
					UserID:      "demo",
					Data: map[string]any{
						"order_id":             "reordered-order",
						"payable_cents":        10000,
						"refunded_total_cents": total,
					},
				}
				for repeat := 0; repeat < 2; repeat++ {
					if err := m.OnEvent(ctx, event); err != nil {
						t.Fatal(err)
					}
				}
			}
			completion := platform.Event{
				Type:        "OrderCompleted",
				AggregateID: "reordered-order",
				UserID:      "demo",
				Data:        map[string]any{"payable_cents": 10000},
			}
			for repeat := 0; repeat < 2; repeat++ {
				if err := m.OnEvent(ctx, completion); err != nil {
					t.Fatal(err)
				}
			}
			credit := must(t, m, "reward", "reordered-order", "demo", "system", map[string]any{"amount": 100})
			if platform.Int(credit, "credited") != 100 {
				t.Fatal("replayed response must retain the gross reward amount", credit)
			}
			v := must(t, m, "get", "", "demo", "user", nil)
			if platform.Int(v, "available") != tc.want || platform.Int(v, "debt") != 0 {
				t.Fatalf("unexpected balance after reordered/duplicate events: %v", v)
			}
			if tc.wantHold != "" {
				var hold struct{ State string }
				if err := db.Table("holds").Where("id = ?", "reordered-order").First(&hold).Error; err != nil ||
					hold.State != tc.wantHold {
					t.Fatalf("refund state regressed: %+v, %v", hold, err)
				}
			}
			if tc.totals[0] < 10000 {
				must(
					t,
					m,
					"refund",
					"remaining-refund",
					"demo",
					"system",
					map[string]any{
						"order_id":             "reordered-order",
						"payable_cents":        10000,
						"refunded_total_cents": 10000,
					},
				)
				v = must(t, m, "get", "", "demo", "user", nil)
				if platform.Int(v, "available") != 5000 {
					t.Fatal("later refund failed to reverse the remaining reward", v)
				}
			}
		})
	}
}
