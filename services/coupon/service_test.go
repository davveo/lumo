package coupon_test

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	service "github.com/davveo/lumo/services/coupon"
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

func TestCouponOwnershipRefundPolicyAndTombstone(t *testing.T) {
	_, m := setup(t, nil)
	if _, e := invoke(
		m,
		"reserve",
		"bad",
		"other",
		"system",
		map[string]any{"coupon_id": "coupon-demo-100", "subtotal_cents": 100000},
	); e == nil {
		t.Fatal("foreign coupon allowed")
	}
	must(t, m, "cancel", "tomb", "demo", "system", nil)
	if _, e := invoke(
		m,
		"reserve",
		"tomb",
		"demo",
		"system",
		map[string]any{"coupon_id": "coupon-demo-100", "subtotal_cents": 100000},
	); e == nil {
		t.Fatal("cancel tombstone lost")
	}
	must(
		t,
		m,
		"reserve",
		"o1",
		"demo",
		"system",
		map[string]any{"coupon_id": "coupon-demo-100", "subtotal_cents": 100000},
	)
	must(t, m, "confirm", "o1", "demo", "system", nil)
	v := must(
		t,
		m,
		"refund",
		"r1",
		"demo",
		"system",
		map[string]any{"order_id": "o1", "payable_cents": 90000, "refunded_total_cents": 30000},
	)
	if platform.Text(v, "status") != "NOT_ELIGIBLE" {
		t.Fatal(v)
	}
	must(
		t,
		m,
		"refund",
		"r2",
		"demo",
		"system",
		map[string]any{"order_id": "o1", "payable_cents": 90000, "refunded_total_cents": 90000},
	)
	must(
		t,
		m,
		"refund",
		"r2",
		"demo",
		"system",
		map[string]any{"order_id": "o1", "payable_cents": 90000, "refunded_total_cents": 90000},
	)
	if _, e := invoke(
		m,
		"reserve",
		"o2",
		"demo",
		"system",
		map[string]any{"coupon_id": "coupon-demo-100", "subtotal_cents": 100000},
	); e != nil {
		t.Fatal("original coupon not restored", e)
	}
}
func TestCouponClaimIdempotencyAndTemplatePolicy(t *testing.T) {
	db, m := setup(t, nil)
	v := must(t, m, "templates", "", "", "", nil)
	if len(v["items"].([]platform.Result)) != 6 || platform.Int(v, "total") != 6 {
		t.Fatal(v)
	}
	if _, e := invoke(m, "claim", "", "", "user", map[string]any{"template_id": "welcome-100"}); e == nil {
		t.Fatal("anonymous claim allowed")
	}
	one := must(t, m, "claim", "", "demo", "user", map[string]any{"template_id": "welcome-100"})
	two := must(t, m, "claim", "", "demo", "user", map[string]any{"template_id": "welcome-100"})
	if one["id"] != two["id"] || two["already_claimed"] != true {
		t.Fatal(one, two)
	}
	var remaining int64
	if e := db.WithContext(context.Background()).Table("templates").Select("remaining").Where(
		"id = ?",
		"welcome-100",
	).Scan(
		&remaining,
	).Error; e != nil {
		t.Fatal(e)
	}
	if remaining != 999 {
		t.Fatalf("remaining=%d", remaining)
	}
	if platform.Int(one, "amount_cents") != 10000 || platform.Int(one, "min_cents") != 100000 {
		t.Fatal(one)
	}
	must(
		t,
		m,
		"reserve",
		"claimed-order",
		"demo",
		"system",
		map[string]any{"coupon_id": one["id"], "subtotal_cents": 100000},
	)
}
func TestCouponConcurrentClaimsRespectQuota(t *testing.T) {
	db, m := setup(t, nil)
	if e := db.WithContext(context.Background()).Table("templates").Where("id = ?", "welcome-100").Update(
		"remaining",
		3,
	).Error; e != nil {
		t.Fatal(e)
	}
	ch := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			_, e := invoke(
				m,
				"claim",
				"",
				fmt.Sprintf("claim-user-%d", i),
				"user",
				map[string]any{"template_id": "welcome-100"},
			)
			ch <- e == nil
		}(i)
	}
	n := 0
	for i := 0; i < 20; i++ {
		if <-ch {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("successful claims=%d", n)
	}
	var remaining int64
	var claims int64
	if e := db.WithContext(context.Background()).Table("templates").Select("remaining").Where(
		"id = ?",
		"welcome-100",
	).Scan(
		&remaining,
	).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.WithContext(context.Background()).Table("claims").Count(&claims).Error; e != nil {
		t.Fatal(e)
	}
	if remaining != 0 || claims != 3 {
		t.Fatal(remaining, claims)
	}
}
