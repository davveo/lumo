package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

// Exercise the same HTTP aggregation used by the rail against real independent
// services. All behavior writes and accounts belong to this temporary cluster.
func TestHTTPRecentSidebarFillsSixDistinctProductsFromOwnedBehavior(t *testing.T) {
	c := newCluster(t)
	counted := &aggregateCountingCaller{next: c.rpc}
	server := httptest.NewServer(httpgateway.NewHandler(counted, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	login := func(name, password string) string {
		return platform.Text(
			h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": password}),
			"token",
		)
	}
	buyer, admin := login("demo", "Lumo@2026"), login("admin", "Admin@2026")
	assertSix := func(result platform.Result) []map[string]any {
		t.Helper()
		items, stocks := mapItems(result), mapItems(platform.Result{"items": result["inventory_items"]})
		if len(items) != 6 || platform.Int(result, "count") != 6 || platform.Int(result, "limit") != 6 ||
			len(stocks) != 6 {
			t.Fatalf("rail did not return six complete real products and stocks: %v", result)
		}
		available := map[string]int64{}
		for _, stock := range stocks {
			available[platform.Text(stock, "sku_id")] = platform.Int(stock, "available")
		}
		seen := map[string]bool{}
		for _, item := range items {
			id := platform.Text(item, "id")
			if id == "" || seen[id] || platform.Text(item, "name") == "" || platform.Int(item, "price_cents") <= 0 ||
				available[id] <= 0 {
				t.Fatalf("duplicate, incomplete or unsellable rail product: %v", item)
			}
			seen[id] = true
			if parent := platform.Text(item, "product_id"); parent != "" && parent != id {
				t.Fatalf("variant consumed a separate product slot: %v", item)
			}
			if platform.Text(item, "recent_source") == "" {
				t.Fatal("rail item omitted its actual source", item)
			}
		}
		return items
	}
	read := func(token string) platform.Result {
		t.Helper()
		return h.call(token, "storefront", "recent", "", "", nil)
	}
	assertGuest := func() {
		t.Helper()
		result := read("")
		if result["personalized"] != false {
			t.Fatal("guest was assigned account preferences", result)
		}
		for _, item := range assertSix(result) {
			if source := platform.Text(item, "recent_source"); source == "history" || source == "favorite" {
				t.Fatal("guest received private behavior-derived items", item)
			}
		}
	}
	assertGuest()
	ids := []string{"iphone", "macbook", "headphones", "watch", "speaker"}
	for n := 0; n <= len(ids); n++ {
		if n > 0 {
			h.call(buyer, "engagement", "put", "", "", map[string]any{"kind": "history", "sku_id": ids[n-1]})
		}
		counted.reset()
		items := assertSix(read(buyer))
		for i := 0; i < n; i++ {
			found := false
			for _, item := range items[:n] {
				if platform.Text(item, "id") == ids[i] && platform.Text(item, "recent_source") == "history" {
					found = true
				}
			}
			if !found {
				t.Fatalf(
					"%d history items were not retained before recommendation fill: missing=%s rows=%v",
					n,
					ids[i],
					items,
				)
			}
		}
		counted.expectCount(t, "engagement", "list", 4)
		counted.expectCount(t, "order", "list", 1)
		counted.expectCount(t, "recommendation", "list", 0)
		counted.expectCount(t, "recommendation", "rank_bootstrap", 1)
		counted.expectCount(t, "catalog", "get", 0)
		counted.expectCount(t, "inventory", "get", 0)
	}
	// Another SKU for a previously viewed product cannot consume the sixth slot.
	h.call(buyer, "engagement", "put", "", "", map[string]any{"kind": "history", "sku_id": "iphone--1-0"})
	h.call(buyer, "engagement", "put", "", "", map[string]any{"kind": "favorite", "sku_id": "coffee"})
	items := assertSix(read(buyer))
	seen := map[string]bool{}
	for _, item := range items {
		seen[platform.Text(item, "id")] = true
	}
	if !seen["coffee"] || !seen["iphone"] || len(seen) != 6 {
		t.Fatal("duplicate variants displaced a different favorite", items)
	}
	// The initial page embeds the same aggregate, without another HTTP request.
	bootstrap := aggregateRows(t, h.call(buyer, "storefront", "bootstrap", "", "", nil))
	assertSix(aggregateFragment(t, bootstrap, "storefront", "recent"))
	h.call(admin, "engagement", "put", "", "", map[string]any{"kind": "history", "sku_id": "perfume"})
	adminItems := assertSix(read(admin))
	if platform.Text(adminItems[0], "id") != "perfume" || platform.Text(adminItems[0], "recent_source") != "history" {
		t.Fatal("another account did not use its own history", adminItems)
	}
	assertGuest()
}
