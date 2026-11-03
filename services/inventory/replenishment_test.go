package inventory_test

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"path/filepath"
	"testing"
)

type supplyPeer struct{}

func (supplyPeer) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	if service == "catalog" {
		var in struct {
			IDs []string `json:"ids"`
		}
		_ = platform.Decode(c.Data, &in)
		items := []platform.Result{}
		for _, id := range in.IDs {
			items = append(items, platform.Result{"id": id, "name": "采购商品"})
		}
		return platform.Result{"items": items}, nil
	}
	return platform.Result{
		"items":          []platform.Result{{"sku_id": "coffee", "quantity": 600}},
		"forecast_ready": false,
		"scope":          "历史不足",
	}, nil
}
func TestReplenishmentSubtractsApprovedInboundAndDoesNotForecastWithoutHistory(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "stock.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	m := inventory.New(platform.Dependencies{DB: db, RPC: supplyPeer{}})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = db.Create(&domain.Stock{SKUID: "coffee", OnHand: 3, Reserved: 1, Threshold: 10}).Error; e != nil {
		t.Fatal(e)
	}
	lines, _ := json.Marshal([]domain.PurchaseLine{{SKUID: "coffee", Quantity: 8, Received: 3}})
	if e = db.Create(&domain.PurchaseOrder{ID: "po-approved", Status: "PARTIALLY_RECEIVED", Lines: string(lines)}).Error; e != nil {
		t.Fatal(e)
	}
	if e = db.Create(&domain.PurchaseOrder{ID: "po-draft", Status: "DRAFT", Lines: string(lines)}).Error; e != nil {
		t.Fatal(e)
	}
	_, e = m.Operations["supplier_upsert"](
		ctx,
		platform.Request{
			ID:     "supplier-1",
			UserID: "admin",
			Role:   "admin",
			Data:   map[string]any{"name": "供货商", "lead_days": 20, "version": 0},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	out, e := m.Operations["replenishment_get"](
		ctx,
		platform.Request{
			UserID: "admin",
			Role:   "admin",
			Data:   map[string]any{"ids": []string{"coffee"}, "supplier_id": "supplier-1", "coverage_days": 14},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	var rows struct {
		Items []platform.Result `json:"items"`
	}
	_ = platform.Decode(out, &rows)
	if len(rows.Items) != 1 || platform.Int(rows.Items[0], "suggested_quantity") != 13 ||
		platform.Int(rows.Items[0], "inbound") != 5 {
		t.Fatal(out)
	}
	var count int64
	db.Model(&domain.PurchaseOrder{}).Count(&count)
	if count != 2 {
		t.Fatal("suggestion created a purchase", count)
	}
}
