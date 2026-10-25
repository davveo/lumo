package tests

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

func TestHTTPRecommendationsUseOwnedPreferencesAndActualSimilarProducts(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	login := func(name, password string) string {
		return platform.Text(
			h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": password}),
			"token",
		)
	}
	buyer, admin := login("demo", "Lumo@2026"), login("admin", "Admin@2026")
	for _, product := range []struct {
		id, name, brand, category string
		price                     int
	}{
		{"rec-orion-one", "Orion 无线音乐耳机", "Orion", "手机数码", 12000},
		{"rec-orion-two", "Orion 无线降噪耳机", "Orion", "手机数码", 13000},
		{"rec-hearth-one", "Hearth 智能蒸汽烤箱", "Hearth", "家用电器", 25000},
		{"rec-hearth-two", "Hearth 智能蒸汽微波炉", "Hearth", "家用电器", 26000},
	} {
		h.call(
			admin,
			"catalog",
			"upsert",
			product.id,
			"",
			map[string]any{
				"name":        product.name,
				"brand":       product.brand,
				"category":    product.category,
				"price_cents": product.price,
				"active":      true,
			},
		)
		h.call(admin, "inventory", "adjust", product.id, "", map[string]any{"on_hand": 20})
	}
	interest := func(token, favorite, history, query string) {
		h.call(token, "engagement", "put", "", "", map[string]any{"kind": "favorite", "sku_id": favorite})
		h.call(token, "engagement", "put", "", "", map[string]any{"kind": "history", "sku_id": history})
		h.call(token, "engagement", "put", "", "", map[string]any{"kind": "search", "query": query})
	}
	interest(buyer, "rec-orion-one", "rec-orion-two", "Orion 耳机")
	interest(admin, "rec-hearth-one", "rec-hearth-two", "Hearth 蒸汽")
	rank := func(token string, data map[string]any) platform.Result {
		result := h.call(token, "recommendation", "list", "", "", data)
		if platform.Text(result, "strategy") != "hybrid_content_v2" || result["personalized_model"] != false {
			t.Fatalf("ranking misrepresented its implementation: %v", result)
		}
		if result["stock_snapshot"] != nil || result["signals"] != nil || result["catalog_items"] != nil {
			t.Fatalf("public recommendation leaked internal input: %v", result)
		}
		return result
	}
	buyerHome := rank(buyer, map[string]any{"scene": "home", "limit": 8})
	adminHome := rank(admin, map[string]any{"scene": "home", "limit": 8})
	first := func(result platform.Result) map[string]any {
		rows := mapItems(result)
		if len(rows) == 0 {
			t.Fatal("empty recommendations", result)
		}
		for _, row := range rows {
			if platform.Text(row, "recommendation_reason") == "" {
				t.Fatal("recommendation has no explanation", row)
			}
		}
		return rows[0]
	}
	if buyerHome["personalized"] != true || adminHome["personalized"] != true ||
		platform.Text(first(buyerHome), "brand") != "Orion" ||
		platform.Text(first(adminHome), "brand") != "Hearth" {
		t.Fatalf("different users did not receive their own ranking: buyer=%v admin=%v", buyerHome, adminHome)
	}
	// An outer validated identity is authoritative even when an admin account
	// exists, and user-supplied signal/catalog snapshots cannot replace it.
	forged := rank(
		buyer,
		map[string]any{
			"scene":         "home",
			"limit":         8,
			"user_id":       "admin",
			"signals":       map[string]any{"favorite": []any{map[string]any{"sku_id": "rec-hearth-one"}}},
			"catalog_items": []any{map[string]any{"id": "fabricated", "price_cents": 1}},
		},
	)
	if platform.Text(first(forged), "id") != platform.Text(first(buyerHome), "id") {
		t.Fatal("nested user/signals changed the recommendation principal", forged)
	}
	guest := rank("", map[string]any{"scene": "home", "limit": 8})
	if guest["personalized"] != false {
		t.Fatal("guest inherited an account profile", guest)
	}
	similar := rank("", map[string]any{"scene": "similar", "product_id": "rec-orion-one", "limit": 8})
	if platform.Text(first(similar), "id") != "rec-orion-two" {
		t.Fatal("similar recommendation did not use actual product features", similar)
	}
	for _, row := range mapItems(similar) {
		if row["id"] == "rec-orion-one" {
			t.Fatal("similar recommendation returned its anchor")
		}
	}
	h.call(admin, "inventory", "adjust", "rec-orion-two", "", map[string]any{"on_hand": 0})
	fresh := rank("", map[string]any{"scene": "similar", "product_id": "rec-orion-one", "limit": 8})
	for _, row := range mapItems(fresh) {
		if row["id"] == "rec-orion-two" {
			t.Fatal("out-of-stock recommendation survived a fresh query")
		}
	}
	// Ranking is requested by its own page section, not hidden in the public shell.
	ranked := rank(buyer, map[string]any{"scene": "home", "limit": 20})
	if ranked["personalized"] != true || platform.Text(first(ranked), "brand") != "Orion" ||
		ranked["stock_snapshot"] != nil {
		t.Fatal("explicit homepage feed lost verified buyer preferences", ranked)
	}
	b, _ := json.Marshal(ranked)
	h.expect(403, admin, "recommendation", "rank_bootstrap", "", "", nil)
	h.expect(403, admin, "catalog", "recommendation_candidates", "", "", nil)
	// No raw profile, token, or source-user identity is sent back with reasons.
	b, _ = json.Marshal(buyerHome)
	if strings.Contains(string(b), buyer) || strings.Contains(string(b), `"user_id":"admin"`) {
		t.Fatal("recommendation output leaked authentication/profile data")
	}
}
