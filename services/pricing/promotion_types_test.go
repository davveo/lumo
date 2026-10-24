package pricing_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	campaignmod "github.com/davveo/lumo/services/campaign"
	pricingmod "github.com/davveo/lumo/services/pricing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type promotionRouter struct {
	modules map[string]platform.Module
	prices  map[string]int64
}

func (r *promotionRouter) Call(ctx context.Context, service string, c platform.Command) (platform.Result, error) {
	if m, ok := r.modules[service]; ok {
		return m.Handle(ctx, c)
	}
	switch service {
	case "catalog":
		row := func(id string) platform.Result {
			return platform.Result{"id": id, "name": "Quoted SKU", "price_cents": r.prices[id], "active": true}
		}
		if c.Action == "batch_get" {
			var data struct {
				IDs []string `json:"ids"`
			}
			if err := platform.Decode(c.Data, &data); err != nil {
				return nil, err
			}
			items := []platform.Result{}
			for _, id := range data.IDs {
				items = append(items, row(id))
			}
			return platform.Result{"items": items, "missing_ids": []string{}}, nil
		}
		return row(c.ID), nil
	case "coupon":
		return platform.Result{"items": []map[string]any{{"id": "coupon-test", "status": "AVAILABLE", "amount_cents": int64(500)}}}, nil
	case "points":
		return platform.Result{"available": int64(5000)}, nil
	case "group":
		return platform.Result{"id": c.ID, "status": "OPEN", "sku_id": "sku-a"}, nil
	}
	return nil, status.Error(codes.Unimplemented, "unexpected promotion dependency")
}

func setupPromotions(t *testing.T) (*promotionRouter, *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	r := &promotionRouter{modules: map[string]platform.Module{}, prices: map[string]int64{"sku-a": 10001, "sku-b": 19999, "sku-coffee": 39900}}
	var campaignDB *gorm.DB
	for name, factory := range map[string]func(platform.Dependencies) platform.Module{"campaign": campaignmod.New, "pricing": pricingmod.New} {
		db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), name+".db"))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = raw.Close() })
		m := factory(platform.Dependencies{DB: db, RPC: r})
		if err := m.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if m.Seed != nil {
			if err := m.Seed(ctx); err != nil {
				t.Fatal(err)
			}
		}
		r.modules[name] = m
		if name == "campaign" {
			campaignDB = db
		}
	}
	return r, campaignDB
}

func (r *promotionRouter) quote(data map[string]any) (platform.Result, error) {
	return r.Call(context.Background(), "pricing", platform.Command{Action: "quote", UserID: "buyer", Role: "user", Data: data})
}

func promotionItems() []any {
	return []any{map[string]any{"sku_id": "sku-a", "quantity": 1, "unit_price_cents": 1}, map[string]any{"sku_id": "sku-b", "quantity": 1}}
}

func TestQuotesApplyAllNewPromotionKindsUsingRealSKUAmounts(t *testing.T) {
	r, _ := setupPromotions(t)
	for _, test := range []struct {
		activity, kind string
		discount       int64
		rate           int64
	}{
		{"reduction-demo", "reduction", 3000, 0},
		{"discount-demo", "discount", 3000, 9000},
		{"quantity-discount-demo", "quantity_discount", 4500, 8500},
		{"direct-demo", "direct", 2000, 0},
	} {
		t.Run(test.kind, func(t *testing.T) {
			q, err := r.quote(map[string]any{"items": promotionItems(), "activity_id": test.activity})
			if err != nil {
				t.Fatal(err)
			}
			if platform.Int(q, "subtotal_cents") != 30000 || platform.Int(q, "campaign_discount_cents") != test.discount || platform.Int(q, "payable_cents") != 30000-test.discount {
				t.Fatalf("quote trusted browser prices or calculated the wrong campaign: %v", q)
			}
			if platform.Text(q, "activity_kind") != test.kind || platform.Text(q, "activity_name") == "" || platform.Int(q, "pay_rate_bps") != test.rate {
				t.Fatalf("quote lacks campaign snapshot metadata: %v", q)
			}
		})
	}
	plain, err := r.quote(map[string]any{"items": promotionItems()})
	if err != nil || platform.Int(plain, "payable_cents") != 30000 || platform.Text(plain, "activity_id") != "" {
		t.Fatalf("new campaigns changed ordinary purchases: %v %v", plain, err)
	}
	stacked, err := r.quote(map[string]any{"items": promotionItems(), "activity_id": "reduction-demo", "coupon_id": "coupon-test", "points": 100})
	if err != nil || platform.Int(stacked, "payable_cents") != 26400 || platform.Int(stacked, "coupon_discount_cents") != 500 {
		t.Fatalf("ordinary coupon/points stacking changed: %v %v", stacked, err)
	}
}

func TestPromotionQuotesStayImmutableWhenRulesAndCatalogChange(t *testing.T) {
	r, db := setupPromotions(t)
	q, err := r.quote(map[string]any{"items": promotionItems(), "activity_id": "discount-demo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("activities").Where("id = ?", "discount-demo").Updates(map[string]any{"pay_rate_bps": 8000, "name": "更新后的活动"}).Error; err != nil {
		t.Fatal(err)
	}
	r.prices["sku-a"] = 40000
	stored, err := r.Call(context.Background(), "pricing", platform.Command{Action: "get", ID: platform.Text(q, "quote_id"), UserID: "buyer", Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if platform.Int(stored, "subtotal_cents") != 30000 || platform.Int(stored, "campaign_discount_cents") != 3000 || platform.Int(stored, "pay_rate_bps") != 9000 || platform.Text(stored, "activity_name") != "折扣特惠" {
		t.Fatalf("quote snapshot changed after a catalog/rule update: %v", stored)
	}
}

func TestPromotionQuotesRejectForgedQuantityAndIncompatibleCampaigns(t *testing.T) {
	r, _ := setupPromotions(t)
	single := []any{map[string]any{"sku_id": "sku-a", "quantity": 1}}
	for _, data := range []map[string]any{
		{"items": single, "activity_id": "quantity-discount-demo", "quantity": 999},
		{"items": single, "activity_id": "discount-demo", "group_id": "team-test"},
		{"items": single, "activity_id": "flash-demo", "group_id": "team-test"},
		{"items": single, "activity_id": "group-demo"},
		{"items": single, "activity_id": "discount-demo", "addon_sku_id": "sku-coffee"},
		{"items": []any{map[string]any{"sku_id": "sku-a", "quantity": 10}}, "activity_id": "addon-coffee"},
	} {
		if q, err := r.quote(data); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("incompatible or forged promotion request accepted: %v %v", q, err)
		}
	}
	group, err := r.quote(map[string]any{"items": single, "group_id": "team-test"})
	if err != nil || platform.Text(group, "activity_id") != "group-demo" || platform.Text(group, "activity_kind") != "group" {
		t.Fatalf("existing group quote no longer works: %v %v", group, err)
	}
	flash, err := r.quote(map[string]any{"items": single, "activity_id": "flash-demo"})
	if err != nil || platform.Int(flash, "campaign_discount_cents") != 0 {
		t.Fatalf("existing flash quote no longer works: %v %v", flash, err)
	}
}
