package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/davveo/lumo/internal/bootstrap"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

const categoryNavigationID = "storefront-categories"

func navigationFixture(t *testing.T) string {
	t.Helper()
	groups := []map[string]any{}
	for _, entry := range []struct {
		id, category string
		enabled      bool
	}{
		{"second", "运营第二类", true}, {"hidden", "运营隐藏类", false}, {"first", "运营第一类", true},
	} {
		groups = append(groups, map[string]any{
			"id": entry.id, "category": entry.category, "label": entry.category + "导航", "title": entry.category,
			"icon": "bag", "enabled": entry.enabled,
			"shortcuts": []map[string]any{{"label": "全部" + entry.category, "query": ""}},
			"rows": []map[string]any{
				{
					"title": "运营分组",
					"query": "配置搜索词",
					"items": []map[string]any{{"label": "运营推荐词", "query": "实际搜索词", "featured": true}},
				},
			},
			"brands": []map[string]any{{"label": "运营品牌", "brand": "KnownBrand"}},
		})
	}
	body, err := json.Marshal(map[string]any{"version": 1, "groups": groups})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func decodedNavigation(t *testing.T, document platform.Result) map[string]any {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal([]byte(platform.Text(document, "body")), &config); err != nil {
		t.Fatalf("navigation content is not backend JSON: %v", err)
	}
	return config
}

func navigationFragment(t *testing.T, rows []map[string]any) platform.Result {
	t.Helper()
	for _, row := range rows {
		if row["service"] == "content" && row["action"] == "get" &&
			platform.Text(platform.Map(row["command"]), "id") == categoryNavigationID {
			if row["auth"] != false || len(platform.Map(platform.Map(row["command"])["data"])) != 0 {
				t.Fatalf("navigation fragment lost the exact public cache scope: %#v", row)
			}
			return aggregateData(t, row)
		}
	}
	t.Fatal("bootstrap did not include category content/get cache entry")
	return nil
}

func TestCategoryNavigationHTTPConfigurationPersistsAndBootstrapsWithoutRepeatedRead(t *testing.T) {
	c := newCluster(t)
	counted := &aggregateCountingCaller{next: c.rpc}
	server := httptest.NewServer(httpgateway.NewHandler(counted, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}

	// The public document is persisted by the content service, not manufactured
	// by the BFF or a frontend fallback.
	document := h.call("", "content", "get", categoryNavigationID, "", nil)
	if document["kind"] != "storefront" || document["status"] != "PUBLISHED" {
		t.Fatalf("navigation content ownership/status changed: %#v", document)
	}
	config := decodedNavigation(t, document)
	if groups, ok := config["groups"].([]any); !ok || len(groups) != 20 {
		t.Fatalf("initial backend taxonomy incomplete: %#v", config)
	}
	counted.reset()
	boot := aggregateRows(t, h.call("", "storefront", "shell", "", "", nil))
	if got := navigationFragment(t, boot); platform.Text(got, "body") != platform.Text(document, "body") {
		t.Fatal("bootstrap changed persisted navigation")
	}
	counted.expectCount(t, "content", "list", 0) // Shell fetches only its two configured documents.
	counted.expectCount(t, "content", "get", 2)

	admin := platform.Text(
		h.call("", "iam", "login", "", "", map[string]any{"username": "admin", "password": "Admin@2026"}),
		"token",
	)
	user := platform.Text(
		h.call("", "iam", "login", "", "", map[string]any{"username": "demo", "password": "Lumo@2026"}),
		"token",
	)
	body := navigationFixture(t)
	update := map[string]any{"kind": "storefront", "title": "后台分类导航", "body": body}
	h.expect(401, "", "content", "upsert", categoryNavigationID, "", update)
	h.expect(403, user, "content", "upsert", categoryNavigationID, "", update)
	h.call(admin, "content", "upsert", categoryNavigationID, "", update)
	h.expect(
		400,
		admin,
		"content",
		"upsert",
		categoryNavigationID,
		"",
		map[string]any{"kind": "storefront", "title": "非法分类", "body": `{"version":1,"groups":"wrong"}`},
	)

	counted.reset()
	boot = aggregateRows(t, h.call("", "storefront", "shell", "", "", nil))
	updated := decodedNavigation(t, navigationFragment(t, boot))
	groups := updated["groups"].([]any)
	if len(groups) != 3 || platform.Text(platform.Map(groups[0]), "id") != "second" ||
		platform.Text(platform.Map(groups[2]), "id") != "first" ||
		platform.Map(groups[1])["enabled"] != false {
		t.Fatalf("backend ordering/disabled group was lost: %#v", groups)
	}
	first := platform.Map(groups[0])
	brands := first["brands"].([]any)
	if platform.Text(platform.Map(brands[0]), "brand") != "KnownBrand" ||
		platform.Text(platform.Map(brands[0]), "label") != "运营品牌" {
		t.Fatal("backend brand config was derived from current product page instead")
	}
	rows := first["rows"].([]any)
	items := platform.Map(rows[0])["items"].([]any)
	if platform.Text(platform.Map(items[0]), "query") != "实际搜索词" || platform.Map(items[0])["featured"] != true {
		t.Fatal("backend recommended term/target was lost")
	}
	counted.expectCount(t, "content", "get", 2)
	for _, call := range counted.snapshot() {
		if call.service == "content" && (call.command.Action == "get" || call.command.Action == "list") &&
			(call.command.UserID != "" || call.command.Role != "") {
			t.Fatal("public configuration read inherited private identity")
		}
	}

	// Reassembly and idempotent migration reuse the same isolated durable store.
	restarted, err := bootstrap.New("content", platform.Dependencies{DB: c.dbs["content"], RPC: c.rpc})
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if restarted.Seed != nil {
		if err = restarted.Seed(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	got := h.call("", "content", "get", categoryNavigationID, "", nil)
	before, _ := json.Marshal(updated)
	after, _ := json.Marshal(decodedNavigation(t, got))
	if string(before) != string(after) {
		t.Fatal("service restart reset administrator configuration")
	}
}

func TestCategoryNavigationBootstrapFindsDocumentBeyondBoundedContentPage(t *testing.T) {
	c := newCluster(t)
	counted := &aggregateCountingCaller{next: c.rpc}
	server := httptest.NewServer(httpgateway.NewHandler(counted, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	// Publish enough newer storefront pages to move the navigation document out
	// of the existing 100-entry editor list. The BFF must not page-walk or lose it.
	for index := 0; index < 101; index++ {
		c.call(
			t,
			context.Background(),
			"content",
			"upsert",
			fmt.Sprintf("newer-storefront-%03d", index),
			"",
			"admin",
			"admin",
			map[string]any{"kind": "storefront", "title": "页面", "body": `{}`},
		)
	}
	counted.reset()
	boot := aggregateRows(t, h.call("", "storefront", "shell", "", "", nil))
	document := navigationFragment(t, boot)
	if platform.Text(document, "id") != categoryNavigationID ||
		len(decodedNavigation(t, document)["groups"].([]any)) != 20 {
		t.Fatal("bounded content list hid the category configuration")
	}
	counted.expectCount(t, "content", "list", 0)
	counted.expectCount(t, "content", "get", 2)
}
