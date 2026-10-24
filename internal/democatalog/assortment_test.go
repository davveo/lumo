package democatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func navigationSource(t *testing.T) ([]byte, assortmentNavigation) {
	t.Helper()
	raw, err := os.ReadFile("../../services/content/internal/usecase/category_navigation.json")
	if err != nil {
		t.Fatal(err)
	}
	var navigation assortmentNavigation
	if err = json.Unmarshal(raw, &navigation); err != nil {
		t.Fatal(err)
	}
	return raw, navigation
}
func searchMatches(product map[string]any, category, query, brand string) bool {
	if product["category"] != category || brand != "" && product["brand"] != brand {
		return false
	}
	needle := strings.ToLower(query)
	for _, key := range []string{"name", "brand", "category"} {
		if strings.Contains(strings.ToLower(product[key].(string)), needle) {
			return true
		}
	}
	return false
}

func TestExpandedAssortmentCoversEveryConfiguredSearch(t *testing.T) {
	_, navigation := navigationSource(t)
	products := ExpandedProducts()
	if len(navigation.Groups) != 20 {
		t.Fatalf("expected current 20-category source; got %d", len(navigation.Groups))
	}
	if len(products) != 1838 {
		t.Fatalf("expected 1758 leaf choices plus 80 independent themed bundles; got %d", len(products))
	}
	categories := map[string]int{}
	matches := func(category, query, brand string) int {
		count := 0
		for _, product := range products {
			if searchMatches(product, category, query, brand) {
				count++
			}
		}
		return count
	}
	for _, product := range products {
		categories[product["category"].(string)]++
	}
	leaves := 0
	for _, group := range navigation.Groups {
		if categories[group.Category] < 72 {
			t.Errorf("category %q has too few demonstration choices: %d", group.Category, categories[group.Category])
		}
		for _, row := range group.Rows {
			query := row.Query
			if query == "" {
				query = row.Title
			}
			if count := matches(group.Category, query, ""); count == 0 {
				t.Errorf("row search empty: %s / %s", group.Category, query)
			}
			for _, item := range row.Items {
				leaves++
				query := item.Query
				if query == "" {
					query = item.Label
				}
				if count := matches(group.Category, query, ""); count < 3 {
					t.Errorf("leaf search has %d options: %s / %s", count, group.Category, query)
				}
			}
		}
		for _, shortcut := range group.Shortcuts {
			if matches(group.Category, shortcut.Query, "") == 0 {
				t.Errorf("shortcut search empty: %s / %s", group.Category, shortcut.Query)
			}
		}
		for _, brand := range group.Brands {
			if matches(group.Category, "", brand.Brand) == 0 {
				t.Errorf("brand filter empty: %s / %s", group.Category, brand.Brand)
			}
		}
	}
	if leaves != 586 {
		t.Fatalf("expected all 586 fine categories; got %d", leaves)
	}
	t.Logf("covered %d categories, %d leaves with %d products and %d independently stocked SKUs", len(categories), leaves, len(products), len(ExpandedSKUs()))
}

func TestAssortmentFixtureTracksBackendSource(t *testing.T) {
	raw, _ := navigationSource(t)
	generated, err := BuildAssortmentFixture(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(generated), bytes.TrimSpace(assortmentJSON)) {
		t.Fatal("demo assortment fixture is stale; run go generate ./internal/democatalog")
	}
	var fixture assortmentFixture
	if err = json.Unmarshal(assortmentJSON, &fixture); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if fixture.SourceSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("fixture does not identify the backend source used to generate it")
	}
}

func TestAssortmentProductsHaveValidVariantsAssetsAndDemoMetadata(t *testing.T) {
	products := ExpandedProducts()
	original := map[string]bool{}
	for _, product := range Products() {
		original[product["id"].(string)] = true
	}
	ids := map[string]bool{}
	for _, product := range products {
		id := product["id"].(string)
		if original[id] || ids[id] || !strings.HasPrefix(id, "demo-assortment-") {
			t.Fatalf("colliding or unscoped product ID: %s", id)
		}
		ids[id] = true
		if product["stock"] != 50 {
			t.Errorf("initial presentation stock must match inventory seed for %s", id)
		}
		if product["demo"] != true || product["demo_source"] != "category-assortment-v1" {
			t.Errorf("missing demo marker for %s", id)
		}
		if !strings.Contains(product["sub"].(string), "演示商品") {
			t.Errorf("missing visible demo disclosure for %s", id)
		}
		price := product["price"].(float64)
		prices := product["prices"].([]any)
		if price < 1 || product["old"].(float64) <= price || len(prices) != 2 || prices[0] != price || prices[1].(float64) <= price {
			t.Errorf("invalid independently priced variant presentation for %s", id)
		}
		if len(product["colors"].([]any)) != 1 || len(product["options"].([]any)) != 2 || product["defaultOption"] != product["options"].([]any)[0] {
			t.Errorf("unexpected variant selection for %s", id)
		}
		image := product["image"].(string)
		local := image
		if !strings.HasPrefix(local, "assets/") {
			local = "assets/" + local
		}
		if _, err := os.Stat(filepath.Join("../../web/dist", local)); err != nil {
			t.Errorf("image not present for %s: %s", id, image)
		}
		if strings.HasSuffix(image, ".svg") && !strings.HasPrefix(image, "assets/demo-") {
			t.Errorf("unexpected SVG path for %s: %s", id, image)
		}
		if strings.Contains(strings.ToLower(product["name"].(string)), "iphone") && product["brand"] != "Apple" {
			t.Errorf("incorrect iPhone brand %v", product["brand"])
		}
	}
	skus := ExpandedSKUs()
	if len(skus) != len(products)*2 {
		t.Fatalf("SKU count does not match two variants per product: %d", len(skus))
	}
	all := map[string]bool{}
	for _, id := range skus {
		if all[id] {
			t.Errorf("duplicate SKU %s", id)
		}
		all[id] = true
	}
	for id := range ids {
		if !all[id] || !all[id+"--0-1"] {
			t.Errorf("stock IDs do not match catalog materialization for %s", id)
		}
	}
}

func TestAssortmentIDsSurviveNavigationReorderingAndReturnFreshCopies(t *testing.T) {
	_, navigation := navigationSource(t)
	original := ExpandedProducts()
	// Navigation ordering is editorial; it must never change persistent item IDs.
	for left, right := 0, len(navigation.Groups)-1; left < right; left, right = left+1, right-1 {
		navigation.Groups[left], navigation.Groups[right] = navigation.Groups[right], navigation.Groups[left]
	}
	for _, group := range navigation.Groups {
		for left, right := 0, len(group.Rows)-1; left < right; left, right = left+1, right-1 {
			group.Rows[left], group.Rows[right] = group.Rows[right], group.Rows[left]
		}
		for _, row := range group.Rows {
			for left, right := 0, len(row.Items)-1; left < right; left, right = left+1, right-1 {
				row.Items[left], row.Items[right] = row.Items[right], row.Items[left]
			}
		}
	}
	encoded, err := json.Marshal(navigation)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := ProductsFromNavigation(encoded)
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]map[string]any{}
	for _, product := range original {
		before[product["id"].(string)] = product
	}
	if len(reordered) != len(original) {
		t.Fatalf("reordering changed product count: %d => %d", len(original), len(reordered))
	}
	for _, product := range reordered {
		if !reflect.DeepEqual(before[product["id"].(string)], product) {
			t.Fatalf("reordering changed product identity or details: %s", product["id"])
		}
	}
	original[0]["name"] = "mutated"
	original[0]["colors"].([]any)[0] = "mutated"
	next := ExpandedProducts()
	if next[0]["name"] == "mutated" || next[0]["colors"].([]any)[0] == "mutated" {
		t.Fatal("demo generators share caller-owned maps/slices")
	}
	if len(Products()) != 12 {
		t.Fatal("expanded assortment must not change the original fixture contract")
	}
}

func TestCurrentConfigurationCanGenerateNewLeafAndShortcutProducts(t *testing.T) {
	raw := []byte(`{"version":1,"groups":[{"id":"pets","category":"宠物生活","rows":[{"title":"猫咪用品","query":"猫咪用品","items":[{"label":"猫窝","query":"猫窝"}]}],"shortcuts":[{"label":"初次养猫","query":"养猫入门"}],"brands":[{"brand":"LUMO"}]}]}`)
	products, err := ProductsFromNavigation(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Three leaf variants plus a meaningful row bundle and shortcut bundle.
	if len(products) != 5 {
		t.Fatalf("expected five demo products, got %d", len(products))
	}
	for _, query := range []string{"猫窝", "猫咪用品", "养猫入门"} {
		found := false
		for _, product := range products {
			if searchMatches(product, "宠物生活", query, "") {
				found = true
			}
		}
		if !found {
			t.Errorf("new configured query %q remains empty", query)
		}
	}
	for _, raw := range [][]byte{[]byte(`{`), []byte(`{"version":2,"groups":[]}`), []byte(`{"version":1}`)} {
		if _, err := ProductsFromNavigation(raw); err == nil {
			t.Errorf("invalid configuration was accepted: %s", raw)
		}
	}
}
