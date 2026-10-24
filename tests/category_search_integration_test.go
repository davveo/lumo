package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

func TestHTTPCategorySearchReturnsActualProductsAndFreshStock(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	admin := platform.Text(h.call("", "iam", "login", "", "", map[string]any{"username": "admin", "password": "Admin@2026"}), "token")
	for _, product := range []struct {
		id, name, category, brand string
		stock                     int
	}{
		{"category-cat-food", "友粮成年猫粮", "宠物生活", "友粮", 3},
		{"category-dog-food", "安心成年狗粮", "宠物生活", "安心", 0},
		{"category-other", "猫粮图鉴", "图书文娱", "友粮", 5},
	} {
		h.call(admin, "catalog", "upsert", product.id, "", map[string]any{"name": product.name, "category": product.category, "brand": product.brand, "price_cents": 1200, "active": true})
		h.call(admin, "inventory", "adjust", product.id, "", map[string]any{"on_hand": product.stock})
	}
	result := h.call("", "search", "query", "", "", map[string]any{"category": "宠物生活", "query": "猫粮", "limit": 100})
	items := mapItems(result)
	stock := mapItems(platform.Result{"items": result["inventory_items"]})
	if len(items) != 1 || items[0]["id"] != "category-cat-food" || items[0]["name"] != "友粮成年猫粮" || len(stock) != 1 || stock[0]["sku_id"] != "category-cat-food" || platform.Int(stock[0], "available") != 3 {
		t.Fatalf("category/name search lost full metadata or authoritative stock: %v", result)
	}
	brand := h.call("", "search", "query", "", "", map[string]any{"category": "宠物生活", "brand": "安心", "limit": 100})
	if rows := mapItems(brand); len(rows) != 1 || rows[0]["id"] != "category-dog-food" {
		t.Fatalf("brand filter escaped its selected department: %v", brand)
	}
	available := func() platform.Result {
		return h.call("", "search", "query", "", "", map[string]any{"category": "宠物生活", "in_stock": true, "limit": 100})
	}
	if rows := mapItems(available()); len(rows) != 1 || rows[0]["id"] != "category-cat-food" {
		t.Fatal("out of stock product survived category filter", rows)
	}
	h.call(admin, "inventory", "adjust", "category-cat-food", "", map[string]any{"on_hand": 0})
	if rows := mapItems(available()); len(rows) != 0 {
		t.Fatal("stock change was hidden by a stale category search", rows)
	}
	for _, query := range []map[string]any{{"category": "健康护理"}, {"category": "宠物生活", "query": "%"}} {
		if rows := mapItems(h.call("", "search", "query", "", "", query)); len(rows) != 0 {
			t.Fatal("empty/literal category query invented a result", rows)
		}
	}
	h.expect(403, "", "catalog", "search_products", "", "", nil)
	h.expect(403, admin, "catalog", "search_products", "", "", nil)
}
