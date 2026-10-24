package catalog_test

import (
	"context"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/democatalog"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDemoAssortmentSeedPreservesAdminProductAndVariantChanges(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	module, db := recallSetup(t)
	ctx := context.Background()
	if err := platform.MigratePlatform(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := module.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	products := democatalog.ExpandedProducts()
	if len(products) == 0 {
		t.Fatal("supplementary demo products missing")
	}
	id := products[0]["id"].(string)
	before, err := module.Handle(ctx, platform.Command{Action: "get", ID: id})
	if err != nil {
		t.Fatal(err)
	}
	showcase, err := module.Handle(ctx, platform.Command{Action: "get", ID: democatalog.Products()[0]["id"].(string)})
	if err != nil {
		t.Fatal(err)
	}
	showcaseTime, err := time.Parse(time.RFC3339, platform.Text(showcase, "created_at"))
	if err != nil {
		t.Fatal(err)
	}
	if platform.Text(before, "created_at") != showcaseTime.Add(-time.Hour).Format(time.RFC3339) {
		t.Fatalf("supplementary fixtures should not displace recent storefront products: %v", before["created_at"])
	}
	if before["demo"] != true || platform.Text(before, "demo_source") != "category-assortment-v1" {
		t.Fatalf("catalog/get lost demo provenance: %v", before)
	}
	matched := searchProducts(t, module, map[string]any{"query": before["name"], "category": before["category"]})
	if len(matched) == 0 || matched[0]["demo"] != true ||
		platform.Text(matched[0], "demo_source") != "category-assortment-v1" {
		t.Fatalf("catalog/search lost demo provenance: %v", matched)
	}
	var presentation struct {
		Variants []struct {
			SKUID string `json:"sku_id"`
		} `json:"variants"`
	}
	if err = platform.Decode(before, &presentation); err != nil {
		t.Fatal(err)
	}
	removed := ""
	for _, variant := range presentation.Variants {
		if variant.SKUID != id {
			removed = variant.SKUID
			break
		}
	}
	if removed == "" {
		t.Fatal("expected an independently priced nondefault SKU")
	}
	changed, err := module.Handle(
		ctx,
		platform.Command{Action: "upsert", ID: id, UserID: "admin", Role: "admin", Data: map[string]any{
			"name": "管理员保留的商品名称", "category": before["category"], "brand": before["brand"], "price_cents": int64(12345),
			"active": false, "colors": []string{"自定义颜色"}, "options": []string{"自定义规格"}, "defaultOption": "自定义规格",
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("DELETE FROM catalog_records WHERE id = ?", removed).Error; err != nil {
		t.Fatal(err)
	}
	var beforeCount int64
	if err = db.Table("catalog_records").Count(&beforeCount).Error; err != nil {
		t.Fatal(err)
	}
	if err = module.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := module.Handle(ctx, platform.Command{Action: "get", ID: id, UserID: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if platform.Text(after, "name") != "管理员保留的商品名称" || platform.Int(after, "price_cents") != 12345 ||
		platform.Text(after, "status") != "DISABLED" ||
		platform.Text(after, "created_at") != platform.Text(changed, "created_at") {
		t.Fatalf("repeated seed overwrote administrator product: %v", after)
	}
	if _, err = module.Handle(ctx, platform.Command{Action: "get", ID: id}); status.Code(err) != codes.NotFound {
		t.Fatalf("disabled product was reactivated: %v", err)
	}
	if _, err = module.Handle(ctx, platform.Command{Action: "get", ID: removed, Role: "admin"}); status.Code(
		err,
	) != codes.NotFound {
		t.Fatalf("removed old variant was recreated: %v", err)
	}
	var afterCount int64
	if err = db.Table("catalog_records").Count(&afterCount).Error; err != nil || afterCount != beforeCount {
		t.Fatalf("repeated seed changed row count %d -> %d: %v", beforeCount, afterCount, err)
	}
}

func TestCatalogAssortmentSeedRequiresDemoMode(t *testing.T) {
	for _, mode := range []string{"", "false", "0"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DEMO_MODE", mode)
			module, db := recallSetup(t)
			if err := module.Seed(context.Background()); err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Table("catalog_records").Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("demo catalog inserted in non-demo mode: count=%d err=%v", count, err)
			}
		})
	}
}
