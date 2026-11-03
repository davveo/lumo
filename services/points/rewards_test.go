package points_test

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	couponmod "github.com/davveo/lumo/services/coupon"
	pointsmod "github.com/davveo/lumo/services/points"
	"github.com/davveo/lumo/services/points/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"testing"
	"time"
)

type rewardRouter struct {
	coupon platform.Module
	calls  map[string]int
	fail   bool
}

func (r *rewardRouter) Call(ctx context.Context, service string, c platform.Command) (platform.Result, error) {
	r.calls[service+"/"+c.Action]++
	if service == "coupon" {
		if r.fail && c.Action == "issue_reward" {
			return nil, status.Error(codes.Unavailable, "temporary outage")
		}
		return r.coupon.Handle(ctx, c)
	}
	if service == "address" {
		return platform.Result{
			"id":        c.ID,
			"user_id":   c.UserID,
			"address":   "测试地址",
			"recipient": "测试",
			"phone":     "13800000000",
		}, nil
	}
	if service == "catalog" {
		return platform.Result{"id": c.ID, "active": true}, nil
	}
	if service == "inventory" || service == "fulfillment" {
		return platform.Result{"id": c.ID}, nil
	}
	return nil, fmt.Errorf("unexpected %s/%s", service, c.Action)
}
func TestRedemptionIsIdempotentAndOldCouponRefundsPoints(t *testing.T) {
	db, m := setup(t, nil)
	coupons := couponmod.New(platform.Dependencies{DB: db})
	if e := coupons.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	r := &rewardRouter{coupon: coupons, calls: map[string]int{}}
	// Recreate transport with the same isolated DB and real coupon repository.
	m = serviceModule(db, r)
	must(
		t,
		coupons,
		"template_upsert",
		"redeem-template",
		"admin",
		"admin",
		map[string]any{
			"name":         "兑换券",
			"amount_cents": 1000,
			"min_cents":    10000,
			"valid_days":   30,
			"remaining":    5,
			"active":       true,
			"expires_at":   time.Now().Add(48 * time.Hour).Format(time.RFC3339),
		},
	)
	offer := domain.RedeemOffer{
		ID:          "reward-test",
		Name:        "测试兑换券",
		Kind:        constants.RewardKindCoupon,
		ReferenceID: "redeem-template",
		Cost:        100,
		TotalQuota:  5,
		MaxPerUser:  1,
		Active:      true,
		StartsAt:    time.Now().Add(-time.Hour),
		EndsAt:      time.Now().Add(time.Hour),
		Version:     1,
	}
	if e := db.Create(&offer).Error; e != nil {
		t.Fatal(e)
	}
	req := platform.Request{
		ID:             offer.ID,
		UserID:         "demo",
		Role:           "user",
		IdempotencyKey: "same-request-key",
		Data:           map[string]any{"version": 1},
	}
	first, e := m.Operations["redeem"](context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	again, e := m.Operations["redeem"](context.Background(), req)
	if e != nil || first["id"] != again["id"] {
		t.Fatal(again, e)
	}
	if got := must(t, m, "get", "", "demo", "user", nil); platform.Int(got, "available") != 4900 {
		t.Fatal(got)
	}
	event := platform.Event{
		Type:        constants.EventRedemptionRequested,
		AggregateID: platform.Text(first, "id"),
		UserID:      "demo",
	}
	r.fail = true
	if e = m.OnEvent(context.Background(), event); e != nil {
		t.Fatal(e)
	}
	if got := must(t, m, "get", "", "demo", "user", nil); platform.Int(got, "available") != 4900 {
		t.Fatal("transient error refunded", got)
	}
	r.fail = false
	if e = m.OnEvent(context.Background(), event); e != nil {
		t.Fatal(e)
	}
	if e = m.OnEvent(context.Background(), event); e != nil {
		t.Fatal(e)
	}
	row := must(t, m, "redemption_get", event.AggregateID, "demo", "user", nil)
	if row["state"] != constants.RedemptionStateIssued || platform.Text(row, "coupon_id") == "" {
		t.Fatal(row)
	}
	if r.calls["coupon/issue_reward"] != 2 {
		t.Fatal(r.calls)
	}
	// A second project pointing to the same template must not charge for the old coupon.
	offer.ID = "reward-test-2"
	offer.Used = 0
	if e = db.Create(&offer).Error; e != nil {
		t.Fatal(e)
	}
	req.ID = offer.ID
	req.IdempotencyKey = "second-request-key"
	second, e := m.Operations["redeem"](context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	event.AggregateID = platform.Text(second, "id")
	if e = m.OnEvent(context.Background(), event); e != nil {
		t.Fatal(e)
	}
	if e = m.OnEvent(context.Background(), event); e != nil {
		t.Fatal(e)
	}
	if got := must(t, m, "get", "", "demo", "user", nil); platform.Int(got, "available") != 4900 {
		t.Fatal("old coupon cost not reversed", got)
	}
	if _, e = invoke(m, "redemption_get", event.AggregateID, "other", "user", nil); status.Code(
		e,
	) != codes.PermissionDenied {
		t.Fatal(e)
	}
}
func TestGiftRedemptionUsesOwnedAddressAndOneInventoryReservation(t *testing.T) {
	db, _ := setup(t, nil)
	r := &rewardRouter{calls: map[string]int{}}
	m := serviceModule(db, r)
	offer := domain.RedeemOffer{
		ID:          "gift",
		Name:        "礼品",
		Kind:        constants.RewardKindGift,
		ReferenceID: "sku-gift",
		Cost:        300,
		TotalQuota:  1,
		MaxPerUser:  1,
		Active:      true,
		StartsAt:    time.Now().Add(-time.Hour),
		EndsAt:      time.Now().Add(time.Hour),
		Version:     1,
	}
	if e := db.Create(&offer).Error; e != nil {
		t.Fatal(e)
	}
	req := platform.Request{
		ID:             offer.ID,
		UserID:         "demo",
		Role:           "user",
		IdempotencyKey: "gift-unique-request",
		Data:           map[string]any{"version": 1, "address_id": "owned"},
	}
	out, e := m.Operations["redeem"](context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.OnEvent(context.Background(), platform.Event{
		Type:        constants.EventRedemptionRequested,
		AggregateID: platform.Text(out, "id"),
		UserID:      "demo",
	}); e != nil {
		t.Fatal(e)
	}
	if r.calls["inventory/reserve"] != 1 || r.calls["inventory/confirm"] != 1 ||
		r.calls["fulfillment/create_gift"] != 1 ||
		r.calls["order/create"] != 0 {
		t.Fatal(r.calls)
	}
	req.IdempotencyKey = "another-gift-request"
	if _, e = m.Operations["redeem"](context.Background(), req); status.Code(e) != codes.ResourceExhausted {
		t.Fatal(e)
	}
}

func serviceModule(db *gorm.DB, r platform.Caller) platform.Module {
	return pointsmod.New(platform.Dependencies{DB: db, RPC: r})
}
