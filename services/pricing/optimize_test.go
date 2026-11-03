package pricing_test

import (
	"context"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/platform"
	pricingmod "github.com/davveo/lumo/services/pricing"
	"path/filepath"
	"testing"
)

type optimizerRouter struct {
	*promotionRouter
	calls map[string]int
}

func (r *optimizerRouter) Call(ctx context.Context, name string, c platform.Command) (platform.Result, error) {
	r.calls[name+"/"+c.Action]++
	if name == "logistics" {
		return platform.Result{
			"shipping_cents": 0,
			"serviceable":    true,
			"policy":         commercepolicy.DefaultShipping(),
		}, nil
	}
	if name == "catalog" && c.Action == "list" {
		return platform.Result{"items": []any{}}, nil
	}
	return r.promotionRouter.Call(ctx, name, c)
}
func TestOptimizerSelectsCompatibleCampaignAndCouponWithoutSpendingPoints(t *testing.T) {
	base, _ := setupPromotions(t)
	r := &optimizerRouter{base, map[string]int{}}
	// Reuse the pricing module's repository via a standalone isolated module.
	m := setupOptimizer(t, r)
	out, e := m.Operations["optimize"](
		context.Background(),
		platform.Request{
			UserID: "buyer",
			Role:   "user",
			Data:   map[string]any{"items": promotionItems(), "coupon_mode": "auto"},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	var got struct {
		Quote platform.Result `json:"quote"`
	}
	if e = platform.Decode(out, &got); e != nil {
		t.Fatal(e)
	}
	if platform.Text(got.Quote, "activity_id") != "quantity-discount-demo" ||
		platform.Int(got.Quote, "payable_cents") != 25000 ||
		platform.Int(got.Quote, "points") != 0 {
		t.Fatalf("bad recommendation %v", out)
	}
	if r.calls["campaign/list"] != 1 || r.calls["catalog/batch_get"] != 2 || r.calls["points/reserve"] != 0 {
		t.Fatal(r.calls)
	}
}
func setupOptimizer(t *testing.T, r platform.Caller) platform.Module {
	t.Helper()
	db, e := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "optimize.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	m := pricingmod.New(platform.Dependencies{DB: db, RPC: r})
	if e = m.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	return m
}
