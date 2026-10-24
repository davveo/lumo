package tests

import (
	"context"
	"fmt"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/davveo/lumo/internal/democatalog"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

// The same persisted navigation used by the storefront must lead to stocked,
// authoritative catalog products. Exercise one cluster rather than booting a
// fresh set of 30 services for each of the hundreds of navigation terms.
func TestDemoAssortmentCoversNavigationAndCanCompletePayment(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	public := func(service, action, id string, data map[string]any) platform.Result {
		return c.call(t, ctx, service, action, id, "", "", "", data)
	}
	navigation := decodedNavigation(t, public("content", "get", categoryNavigationID, nil))
	groups := navigation["groups"].([]any)
	if len(groups) != 20 {
		t.Fatalf("unexpected default navigation breadth: %d", len(groups))
	}
	leafCount, rowCount, shortcutCount, brandCount := 0, 0, 0, 0
	for _, value := range groups {
		group := platform.Map(value)
		category := platform.Text(group, "category")
		t.Run(category, func(t *testing.T) {
			query := func(keyword, brand string, minimum int) {
				t.Helper()
				result := c.call(t, ctx, "search", "query", "", "", "", "", map[string]any{
					"category": category, "query": keyword, "brand": brand, "in_stock": true, "limit": 3,
				})
				items := mapItems(result)
				if len(items) < minimum {
					t.Fatalf(
						"navigation target has insufficient actual products: category=%q query=%q brand=%q got=%d want>=%d",
						category,
						keyword,
						brand,
						len(items),
						minimum,
					)
				}
				stocks := mapItems(platform.Result{"items": result["inventory_items"]})
				available := map[string]int64{}
				for _, stock := range stocks {
					available[platform.Text(stock, "sku_id")] = platform.Int(stock, "available")
				}
				for _, product := range items {
					id := platform.Text(product, "id")
					if platform.Text(product, "category") != category || platform.Int(product, "price_cents") < 1 ||
						platform.Text(product, "image") == "" ||
						available[id] < 1 {
						t.Fatalf("search result lost category, price, image or live stock: %v", product)
					}
					if brand != "" && platform.Text(product, "brand") != brand {
						t.Fatalf("configured brand escaped its filter: %v", product)
					}
				}
			}
			query("", "", 3)
			for _, value := range group["shortcuts"].([]any) {
				shortcutCount++
				query(platform.Text(platform.Map(value), "query"), "", 1)
			}
			for _, value := range group["rows"].([]any) {
				row := platform.Map(value)
				rowCount++
				query(platform.Text(row, "query"), "", 1)
				for _, value := range row["items"].([]any) {
					leafCount++
					query(platform.Text(platform.Map(value), "query"), "", 3)
				}
			}
			for _, value := range group["brands"].([]any) {
				brandCount++
				query("", platform.Text(platform.Map(value), "brand"), 1)
			}
		})
	}
	if leafCount != 586 || rowCount != 93 {
		t.Fatalf("navigation coverage unexpectedly shrank: leaves=%d rows=%d", leafCount, rowCount)
	}
	t.Logf(
		"actual search covered %d departments, %d rows, %d leaves, %d shortcuts and %d configured brands",
		len(groups),
		rowCount,
		leafCount,
		shortcutCount,
		brandCount,
	)

	products := democatalog.ExpandedProducts()
	ids := make([]string, 0, len(products))
	expected := map[string]map[string]any{}
	for _, product := range products {
		id := platform.Text(product, "id")
		ids = append(ids, id)
		expected[id] = product
	}
	if len(products) < leafCount*3 || len(expected) != len(products) {
		t.Fatalf(
			"expanded assortment is insufficient or has duplicate IDs: products=%d unique=%d",
			len(products),
			len(expected),
		)
	}
	variants := map[string]bool{}
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		result := public("catalog", "batch_get", "", map[string]any{"ids": ids[start:end]})
		items := mapItems(result)
		if len(items) != end-start {
			t.Fatalf("expanded catalog batch is incomplete: %d..%d returned %d", start, end, len(items))
		}
		for _, product := range items {
			id := platform.Text(product, "id")
			seed := expected[id]
			wantCents := int64(math.Round(seed["price"].(float64) * 100))
			if platform.Text(product, "name") != platform.Text(seed, "name") ||
				platform.Text(product, "category") != platform.Text(seed, "category") ||
				platform.Int(product, "price_cents") != wantCents {
				t.Fatalf("catalog materialization differs from the development definition: %v", product)
			}
			for _, variant := range mapItems(platform.Result{"items": product["variants"]}) {
				sku := platform.Text(variant, "sku_id")
				if sku == "" || variants[sku] || platform.Int(variant, "price_cents") < 1 {
					t.Fatalf("invalid independently priced variant: %v", variant)
				}
				variants[sku] = true
			}
		}
	}
	skus := democatalog.ExpandedSKUs()
	if len(variants) != len(skus) {
		t.Fatalf("catalog and inventory seed definitions disagree: catalog=%d inventory=%d", len(variants), len(skus))
	}
	for start := 0; start < len(skus); start += 500 {
		end := min(start+500, len(skus))
		result := public("inventory", "batch_get", "", map[string]any{"ids": skus[start:end]})
		stocks := mapItems(result)
		if len(stocks) != end-start {
			t.Fatalf("expanded inventory batch is incomplete: %d..%d returned %d", start, end, len(stocks))
		}
		for _, stock := range stocks {
			if !variants[platform.Text(stock, "sku_id")] || platform.Int(stock, "on_hand") < 1 ||
				platform.Int(stock, "available") < 1 {
				t.Fatalf("expanded SKU is not backed by usable stock: %v", stock)
			}
		}
	}

	// Complete a purchase of a non-default expanded SKU through the real public
	// HTTP gateway. An event pump verifies that demo data has all dependencies
	// required by the existing order/payment/warehouse chain.
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	token := platform.Text(
		h.call("", "iam", "login", "", "", map[string]any{"username": "demo", "password": "Lumo@2026"}),
		"token",
	)
	defer h.call(token, "iam", "logout", "", "", nil)
	sku := skus[len(skus)-1]
	stockBefore := h.call("", "inventory", "get", sku, "", nil)
	h.call(token, "cart", "add", "", "", map[string]any{"sku_id": sku, "quantity": 1})
	quote := h.call(
		token,
		"pricing",
		"quote",
		"",
		"",
		map[string]any{"items": []any{map[string]any{"sku_id": sku, "quantity": 1}}},
	)
	if platform.Int(quote, "subtotal_cents") < 1 || platform.Int(quote, "payable_cents") < 1 {
		t.Fatalf("expanded variant has no authoritative quote: %v", quote)
	}
	orderID := platform.Text(h.call(token, "checkout", "submit", "", "demo-assortment-purchase", map[string]any{
		"quote_id": platform.Text(quote, "quote_id"), "source": "cart",
		"address": map[string]any{
			"recipient": "Assortment Buyer",
			"phone":     "13800000000",
			"address":   "Integration Assortment Street",
		},
	}), "order_id")
	payment := h.call(token, "payment", "create", orderID, "", nil)
	h.call(token, "payment", "submit_demo", platform.Text(payment, "id"), "", nil)
	c.pump(t)
	order := h.call(token, "order", "get", orderID, "", nil)
	warehouse := h.call(token, "fulfillment", "get", orderID, "", nil)
	if platform.Text(order, "status") != "READY_TO_FULFILL" || platform.Text(warehouse, "status") != "READY" {
		t.Fatalf(
			"expanded variant could not finish payment and resource confirmation: order=%v warehouse=%v",
			order,
			warehouse,
		)
	}
	stockPaid := h.call("", "inventory", "get", sku, "", nil)
	if platform.Int(stockPaid, "allocated") != platform.Int(stockBefore, "allocated")+1 ||
		platform.Int(stockPaid, "available") != platform.Int(stockBefore, "available")-1 {
		t.Fatalf("expanded purchase did not allocate real stock: before=%v after=%v", stockBefore, stockPaid)
	}
	for _, name := range []string{"catalog", "inventory"} {
		if err := c.modules[name].Seed(ctx); err != nil {
			t.Fatal(fmt.Sprintf("repeat %s seed", name), err)
		}
	}
	stockRestart := h.call("", "inventory", "get", sku, "", nil)
	for _, field := range []string{"on_hand", "reserved", "allocated", "available"} {
		if platform.Int(stockRestart, field) != platform.Int(stockPaid, field) {
			t.Fatalf("repeated seed reset purchased stock field %s: before=%v after=%v", field, stockPaid, stockRestart)
		}
	}
}
