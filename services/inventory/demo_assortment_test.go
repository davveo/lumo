package inventory_test

import (
	"context"
	"testing"

	"github.com/davveo/lumo/internal/democatalog"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDemoAssortmentStockSeedPreservesCommittedResourcesAndLedger(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	db, module := setup(t, nil)
	ids := democatalog.ExpandedSKUs()
	if len(ids) < 2 {
		t.Fatal("expanded stock definitions missing")
	}
	for _, id := range []string{ids[0], ids[1], ids[len(ids)-1]} {
		stock := must(t, module, "get", id, "", "", nil)
		if platform.Int(stock, "available") != 50 {
			t.Fatalf("missing initial expanded stock: %v", stock)
		}
	}
	must(t, module, "adjust", ids[0], "admin", "admin", map[string]any{"on_hand": 7})
	must(t, module, "reserve", "demo-seed-preserve", "buyer", "system", map[string]any{"order_id": "demo-seed-order", "lines": []map[string]any{{"sku_id": ids[0], "quantity": 2}}})
	must(t, module, "confirm", "demo-seed-preserve", "buyer", "system", nil)
	must(t, module, "adjust", ids[1], "admin", "admin", map[string]any{"on_hand": 0})
	var beforeCount int64
	if err := db.Table("ledgers").Count(&beforeCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := module.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	stock := must(t, module, "get", ids[0], "", "", nil)
	if platform.Int(stock, "on_hand") != 7 || platform.Int(stock, "allocated") != 2 || platform.Int(stock, "reserved") != 0 || platform.Int(stock, "available") != 5 {
		t.Fatalf("seed reset real committed resources: %v", stock)
	}
	stock = must(t, module, "get", ids[1], "", "", nil)
	if platform.Int(stock, "available") != 0 {
		t.Fatalf("seed restocked sold-out SKU: %v", stock)
	}
	var afterCount int64
	if err := db.Table("ledgers").Count(&afterCount).Error; err != nil || beforeCount != afterCount {
		t.Fatalf("seed changed inventory ledger %d -> %d: %v", beforeCount, afterCount, err)
	}
}

func TestExpandedStockIsNotSeededOutsideDemoMode(t *testing.T) {
	t.Setenv("DEMO_MODE", "false")
	db, module := setup(t, nil)
	id := democatalog.ExpandedSKUs()[0]
	if _, err := invoke(module, "get", id, "", "", nil); status.Code(err) != codes.NotFound {
		t.Fatalf("non-demo mode received expanded fixture stock: %v", err)
	}
	var count int64
	if err := db.Table("stocks").Count(&count).Error; err != nil || count != int64(len(democatalog.SKUs())+4) {
		t.Fatalf("existing basic stock behavior changed: %d %v", count, err)
	}
}
