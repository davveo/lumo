package content_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/content"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func categoryModule(t *testing.T, path string) (*gorm.DB, platform.Module) {
	t.Helper()
	db, err := platform.OpenDB(context.Background(), "sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw, _ := db.DB(); _ = raw.Close() })
	module := content.New(platform.Dependencies{DB: db})
	if err := module.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db, module
}

func categoryGet(t *testing.T, module platform.Module) platform.Result {
	t.Helper()
	result, err := module.Handle(context.Background(), platform.Command{Action: "get", ID: "storefront-categories"})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func categoryBody(t *testing.T, raw platform.Result) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(platform.Text(raw, "body")), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func categoryFixture() map[string]any {
	return map[string]any{"version": 1, "groups": []any{map[string]any{
		"id": "configured", "category": "配置品类", "label": "运营配置", "title": "配置品类", "icon": "bag", "enabled": true,
		"shortcuts": []any{map[string]any{"label": "全部配置品类", "query": ""}},
		"rows": []any{
			map[string]any{
				"title": "测试分组",
				"query": "",
				"items": []any{map[string]any{"label": "搜索词", "query": "", "featured": true}},
			},
		},
		"brands": []any{map[string]any{"label": "运营品牌", "brand": "KnownBrand"}},
	}}}
}

func categoryGroup(config map[string]any) map[string]any {
	return config["groups"].([]any)[0].(map[string]any)
}
func categoryRow(config map[string]any) map[string]any {
	return categoryGroup(config)["rows"].([]any)[0].(map[string]any)
}
func categoryItem(config map[string]any) map[string]any {
	return categoryRow(config)["items"].([]any)[0].(map[string]any)
}
func categoryJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func categorySave(module platform.Module, id, kind, body, user, role string) (platform.Result, error) {
	return module.Handle(
		context.Background(),
		platform.Command{
			Action: "upsert",
			ID:     id,
			UserID: user,
			Role:   role,
			Data:   map[string]any{"kind": kind, "title": "商城分类导航", "body": body},
		},
	)
}

func TestCategoryNavigationInitializesOutsideDemoAndPersistsAdministratorConfiguration(t *testing.T) {
	t.Setenv("DEMO_MODE", "false")
	path := filepath.Join(t.TempDir(), "content.db")
	db, module := categoryModule(t, path)
	initial := categoryGet(t, module)
	if platform.Text(initial, "kind") != "storefront" || platform.Text(initial, "status") != "PUBLISHED" {
		t.Fatal(initial)
	}
	groups := categoryBody(t, initial)["groups"].([]any)
	rows, items := 0, 0
	for _, raw := range groups {
		group := raw.(map[string]any)
		for _, raw := range group["rows"].([]any) {
			rows++
			items += len(raw.(map[string]any)["items"].([]any))
		}
	}
	if len(groups) != 20 || rows != 93 || items != 586 {
		t.Fatalf("default navigation lost existing choices: groups=%d rows=%d items=%d", len(groups), rows, items)
	}
	fixture := categoryFixture()
	categoryGroup(fixture)["enabled"] = false
	saved, err := categorySave(
		module,
		"storefront-categories",
		"storefront",
		categoryJSON(t, fixture),
		"admin",
		"admin",
	)
	if err != nil {
		t.Fatal(err)
	}
	savedBody := platform.Text(saved, "body")
	parsed := categoryBody(t, saved)
	if categoryRow(parsed)["query"] != "测试分组" || categoryItem(parsed)["query"] != "搜索词" ||
		categoryGroup(parsed)["enabled"] != false {
		t.Fatalf("normalization or visibility lost: %v", parsed)
	}
	if platform.Text(saved, "created_at") != platform.Text(initial, "created_at") {
		t.Fatal("update replaced document creation time")
	}
	raw, _ := db.DB()
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	_, restarted := categoryModule(t, path)
	if got := platform.Text(categoryGet(t, restarted), "body"); got != savedBody {
		t.Fatal("startup overwrote administrator category configuration")
	}
	if err := restarted.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := platform.Text(categoryGet(t, restarted), "body"); got != savedBody {
		t.Fatal("demo seed overwrote category configuration")
	}
}

func TestCategoryNavigationRequiresAdministratorAndProtectsDocumentIdentity(t *testing.T) {
	_, module := categoryModule(t, filepath.Join(t.TempDir(), "content.db"))
	before := platform.Text(categoryGet(t, module), "body")
	body := categoryJSON(t, categoryFixture())
	for _, test := range []struct {
		user, role, id, kind string
		code                 codes.Code
	}{
		{"", "admin", "storefront-categories", "storefront", codes.Unauthenticated},
		{"buyer", "user", "storefront-categories", "storefront", codes.PermissionDenied},
		{"admin", "admin", "storefront-categories", "faq", codes.InvalidArgument},
		{"admin", "admin", "another-document", "category_navigation", codes.InvalidArgument},
		{"admin", "admin", "another-document", "categories", codes.InvalidArgument},
	} {
		_, err := categorySave(module, test.id, test.kind, body, test.user, test.role)
		if status.Code(err) != test.code {
			t.Fatalf("unexpected permission/identity result: %+v %v", test, err)
		}
	}
	if got := platform.Text(categoryGet(t, module), "body"); got != before {
		t.Fatal("rejected write changed published navigation")
	}
}

func TestCategoryNavigationValidatesShapeBoundsAndUniqueness(t *testing.T) {
	_, module := categoryModule(t, filepath.Join(t.TempDir(), "content.db"))
	before := platform.Text(categoryGet(t, module), "body")
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"unsupported-version", func(c map[string]any) { c["version"] = 2 }},
		{"missing-groups", func(c map[string]any) { delete(c, "groups") }},
		{"unknown-field", func(c map[string]any) { c["script"] = "alert(1)" }},
		{"invalid-id", func(c map[string]any) { categoryGroup(c)["id"] = `bad\"selector` }},
		{"duplicate-id", func(c map[string]any) {
			other := categoryGroup(categoryFixture())
			other["category"] = "别的品类"
			c["groups"] = append(c["groups"].([]any), other)
		}},
		{"duplicate-category", func(c map[string]any) {
			other := categoryGroup(categoryFixture())
			other["id"] = "other"
			c["groups"] = append(c["groups"].([]any), other)
		}},
		{"reserved-category", func(c map[string]any) { categoryGroup(c)["category"] = "全部好物" }},
		{"invalid-icon", func(c map[string]any) { categoryGroup(c)["icon"] = "<svg onload=alert(1)>" }},
		{
			"disabled-invalid-icon",
			func(c map[string]any) { categoryGroup(c)["enabled"] = false; categoryGroup(c)["icon"] = "bad" },
		},
		{"invalid-enabled", func(c map[string]any) { categoryGroup(c)["enabled"] = "false" }},
		{"null-enabled", func(c map[string]any) { categoryGroup(c)["enabled"] = nil }},
		{"invalid-featured", func(c map[string]any) { categoryItem(c)["featured"] = "false" }},
		{"null-featured", func(c map[string]any) { categoryItem(c)["featured"] = nil }},
		{"missing-rows", func(c map[string]any) { delete(categoryGroup(c), "rows") }},
		{"missing-items", func(c map[string]any) { delete(categoryRow(c), "items") }},
		{"long-label", func(c map[string]any) { categoryItem(c)["label"] = strings.Repeat("字", 81) }},
		{"long-query", func(c map[string]any) { categoryItem(c)["query"] = strings.Repeat("字", 121) }},
		{"control-text", func(c map[string]any) { categoryRow(c)["title"] = "分\x00组" }},
		{"duplicate-row", func(c map[string]any) {
			categoryGroup(c)["rows"] = append(categoryGroup(c)["rows"].([]any), categoryRow(categoryFixture()))
		}},
		{"duplicate-item", func(c map[string]any) {
			categoryRow(c)["items"] = append(categoryRow(c)["items"].([]any), categoryItem(categoryFixture()))
		}},
		{"duplicate-shortcut", func(c map[string]any) {
			categoryGroup(c)["shortcuts"] = []any{
				map[string]any{"label": "重复", "query": "a"},
				map[string]any{"label": "重复", "query": "b"},
			}
		}},
		{"duplicate-brand", func(c map[string]any) {
			categoryGroup(c)["brands"] = []any{
				map[string]any{"label": "一", "brand": "same"},
				map[string]any{"label": "二", "brand": "same"},
			}
		}},
		{"too-many-groups", func(c map[string]any) {
			groups := []any{}
			for i := 0; i < 41; i++ {
				g := categoryGroup(categoryFixture())
				g["id"] = fmt.Sprintf("g%d", i)
				g["category"] = fmt.Sprintf("品类%d", i)
				g["rows"] = []any{}
				groups = append(groups, g)
			}
			c["groups"] = groups
		}},
		{"too-many-rows", func(c map[string]any) {
			rows := []any{}
			for i := 0; i < 21; i++ {
				rows = append(rows, map[string]any{"title": fmt.Sprintf("r%d", i), "query": "", "items": []any{}})
			}
			categoryGroup(c)["rows"] = rows
		}},
		{"too-many-items", func(c map[string]any) {
			items := []any{}
			for i := 0; i < 41; i++ {
				items = append(items, map[string]any{"label": fmt.Sprintf("i%d", i), "query": ""})
			}
			categoryRow(c)["items"] = items
		}},
		{"too-many-shortcuts", func(c map[string]any) {
			items := []any{}
			for i := 0; i < 9; i++ {
				items = append(items, map[string]any{"label": fmt.Sprintf("i%d", i), "query": ""})
			}
			categoryGroup(c)["shortcuts"] = items
		}},
		{"too-many-brands", func(c map[string]any) {
			items := []any{}
			for i := 0; i < 17; i++ {
				items = append(items, map[string]any{"label": fmt.Sprintf("i%d", i), "brand": fmt.Sprintf("i%d", i)})
			}
			categoryGroup(c)["brands"] = items
		}},
		{"too-many-total-rows", func(c map[string]any) {
			groups := []any{}
			for i := 0; i < 21; i++ {
				group := categoryGroup(categoryFixture())
				group["id"] = fmt.Sprintf("g%d", i)
				group["category"] = fmt.Sprintf("品类%d", i)
				rows := []any{}
				for j := 0; j < 20; j++ {
					rows = append(rows, map[string]any{"title": fmt.Sprintf("r%d", j), "query": "", "items": []any{}})
				}
				group["rows"] = rows
				groups = append(groups, group)
			}
			c["groups"] = groups
		}},
		{"too-many-total-items", func(c map[string]any) {
			groups := []any{}
			for i := 0; i < 3; i++ {
				group := categoryGroup(categoryFixture())
				group["id"] = fmt.Sprintf("g%d", i)
				group["category"] = fmt.Sprintf("品类%d", i)
				rows := []any{}
				for j := 0; j < 20; j++ {
					items := []any{}
					for k := 0; k < 34; k++ {
						items = append(items, map[string]any{"label": fmt.Sprintf("i%d", k), "query": ""})
					}
					rows = append(rows, map[string]any{"title": fmt.Sprintf("r%d", j), "query": "", "items": items})
				}
				group["rows"] = rows
				groups = append(groups, group)
			}
			c["groups"] = groups
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := categoryFixture()
			test.mutate(fixture)
			_, err := categorySave(
				module,
				"storefront-categories",
				"storefront",
				categoryJSON(t, fixture),
				"admin",
				"admin",
			)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("invalid category config accepted: %v", err)
			}
		})
	}
	for _, body := range []string{
		"{",
		`{"version":1,"groups":[]} {}`,
		strings.Repeat(" ", 256<<10) + `{"version":1,"groups":[]}`,
	} {
		if _, err := categorySave(module, "storefront-categories", "storefront", body, "admin", "admin"); status.Code(
			err,
		) != codes.InvalidArgument {
			t.Fatalf("invalid/oversized document accepted: %v", err)
		}
	}
	if got := platform.Text(categoryGet(t, module), "body"); got != before {
		t.Fatal("invalid input altered published config")
	}
}

func TestCategoryNavigationAllowsExplicitEmptyConfigurationAndMissingOptionalFlags(t *testing.T) {
	_, module := categoryModule(t, filepath.Join(t.TempDir(), "content.db"))
	for _, fixture := range []map[string]any{{"version": 1, "groups": []any{}}, categoryFixture()} {
		if len(fixture["groups"].([]any)) > 0 {
			group := categoryGroup(fixture)
			delete(group, "enabled")
			delete(group, "brands")
			delete(group, "shortcuts")
			delete(categoryItem(fixture), "featured")
		}
		result, err := categorySave(
			module,
			"storefront-categories",
			"storefront",
			categoryJSON(t, fixture),
			"admin",
			"admin",
		)
		if err != nil {
			t.Fatal(err)
		}
		parsed := categoryBody(t, result)
		if groups := parsed["groups"].([]any); len(groups) > 0 {
			group := groups[0].(map[string]any)
			if _, present := group["enabled"]; present {
				t.Fatal("missing enabled no longer has its optional default")
			}
			if group["brands"] == nil || group["shortcuts"] == nil {
				t.Fatal("optional collection defaults must be arrays")
			}
		}
	}
}
