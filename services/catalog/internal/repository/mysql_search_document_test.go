package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"gorm.io/gorm"
)

func mysqlSearchScratch(t *testing.T) (context.Context, *gorm.DB) {
	t.Helper()
	if os.Getenv("LUMO_MYSQL_SEARCH_TEST") != "1" {
		t.Skip("requires explicitly enabled local MySQL scratch-schema test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	password := os.Getenv("MYSQL_ROOT_PASSWORD")
	if password == "" {
		t.Fatal("local MySQL test credential missing")
	}
	dsn := "root:" + password + "@tcp(127.0.0.1:3306)/"
	admin, err := platform.OpenDB(ctx, "mysql", dsn+"?charset=utf8mb4&parseTime=true&loc=UTC")
	if err != nil {
		t.Fatal("connect scratch administrator", err)
	}
	rawAdmin, _ := admin.DB()
	t.Cleanup(func() { _ = rawAdmin.Close() })
	name := fmt.Sprintf("lumo_search_document_probe_%d", time.Now().UnixNano())
	if err = admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE " + name).Error; err != nil {
			t.Error("cleanup disposable schema", err)
		}
	})
	db, err := platform.OpenDB(ctx, "mysql", dsn+name+"?charset=utf8mb4&parseTime=true&loc=UTC")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	if err := db.AutoMigrate(&domain.Record{}); err != nil {
		t.Fatal(err)
	}
	return ctx, db
}

func TestMySQLSearchDocumentPreservesExactSearchAndUpdates(t *testing.T) {
	ctx, db := mysqlSearchScratch(t)
	r := New(db)
	if err := r.Migrate(ctx); err != nil {
		var generated []struct{ Name, Expression, Extra, Kind string }
		_ = db.Raw("SELECT COLUMN_NAME AS name, GENERATION_EXPRESSION AS expression, EXTRA AS extra, DATA_TYPE AS kind FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND COLUMN_NAME LIKE 'search_%_v2'").Scan(&generated).Error
		for _, value := range generated {
			t.Logf("generated %s type=%s extra=%s expression=%s", value.Name, value.Kind, value.Extra, value.Expression)
		}
		t.Fatal("online search migration", err)
	}
	if err := r.Migrate(ctx); err != nil {
		t.Fatal("idempotent search migration", err)
	}
	if !r.searchDocumentIndexed {
		t.Fatal("verified covering search index not enabled")
	}
	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []struct {
		id, state, parent, raw string
	}{
		{"short-a", "ACTIVE", "", `{"name":"ab","brand":"cd","category":"生活电器","price_cents":150,"sales":7}`},
		{"short-b", "ACTIVE", "", `{"name":"养生壶 100%_! Cotton","brand":"CaseBrand","category":"生活电器","price_cents":300,"sales":2}`},
		{"short-c", "ACTIVE", "", `{"name":"ÉLAN Foo","brand":"Other","category":"生活电器","price_cents":"250","sales":"9"}`},
		{"json-values", "ACTIVE", "", `{"name":null,"brand":77,"category":["家具"],"price_cents":"invalid","sales":null}`},
		{"missing-values", "ACTIVE", "", `{}`},
		{"long-name", "ACTIVE", "", fmt.Sprintf(`{"name":%q,"brand":"CaseBrand","category":"生活电器","price_cents":100,"sales":99}`, strings.Repeat("长", 320)+"养生壶")},
		{"long-brand", "ACTIVE", "", fmt.Sprintf(`{"name":"养生壶","brand":%q,"category":"生活电器","price_cents":500,"sales":1}`, strings.Repeat("B", 81)+"猫粮")},
		{"long-number", "ACTIVE", "", fmt.Sprintf(`{"name":"养生壶","brand":"Other","category":"生活电器","price_cents":%q,"sales":"11"}`, strings.Repeat("9", 40))},
		{"hidden", "ACTIVE", "", `{"name":"养生壶","catalog_visible":false}`},
		{"hidden-uppercase", "ACTIVE", "", `{"name":"养生壶","catalog_visible":"FALSE"}`},
		{"hidden-accent", "ACTIVE", "", `{"name":"养生壶","catalog_visible":"fálse"}`},
		{"disabled", "DISABLED", "", `{"name":"养生壶"}`},
		{"child", "ACTIVE", "parent", `{"name":"养生壶"}`},
		{"broken", "ACTIVE", "", `not JSON`},
	}
	for index, fixture := range fixtures {
		row := domain.Record{ID: fixture.id, Kind: "sku", Status: fixture.state, ParentID: fixture.parent, Payload: fixture.raw, CreatedAt: created.Add(time.Duration(index%3) * time.Second)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal("insert semantic fixture", fixture.id, err)
		}
	}
	ids := func(rows []domain.Record) []string {
		result := []string{}
		for _, row := range rows {
			result = append(result, row.ID)
		}
		return result
	}
	for _, query := range []string{"", "养生壶", "bc", "b c", "100%_!", "casebrand", "ÉLAN", "77", "null", "家具", "猫粮"} {
		for _, sort := range []string{"", "newest", "price_asc", "price_desc", "sales"} {
			for _, limit := range []int{1, 3, 100} {
				input := ProductSearch{Query: query, Sort: sort, Limit: limit}
				r.searchDocumentIndexed = false
				legacy, legacyErr := r.SearchProducts(ctx, input)
				r.searchDocumentIndexed = true
				indexed, indexedErr := r.SearchProducts(ctx, input)
				if legacyErr != nil || indexedErr != nil || !reflect.DeepEqual(ids(legacy), ids(indexed)) {
					t.Fatalf("search semantics changed q=%q sort=%s limit=%d: legacy=%v/%v indexed=%v/%v", query, sort, limit, ids(legacy), legacyErr, ids(indexed), indexedErr)
				}
			}
		}
	}
	for _, input := range []ProductSearch{{Category: "生活电器", Brand: "CaseBrand", Limit: 100}, {Category: "生活电器", Brand: "casebrand", Limit: 100}, {Query: "养生壶", Category: "生活电器", Sort: "price_asc", Limit: 2}} {
		r.searchDocumentIndexed = false
		legacy, legacyErr := r.SearchProducts(ctx, input)
		r.searchDocumentIndexed = true
		indexed, indexedErr := r.SearchProducts(ctx, input)
		if legacyErr != nil || indexedErr != nil || !reflect.DeepEqual(ids(legacy), ids(indexed)) {
			t.Fatalf("exact filters changed: %v legacy=%v/%v indexed=%v/%v", input, ids(legacy), legacyErr, ids(indexed), indexedErr)
		}
	}
	if err := db.Model(&domain.Record{}).Where("id = ?", "short-b").Update("payload", `{"name":"已改名猫粮","category":"宠物生活","price_cents":123}`).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := r.SearchProducts(ctx, ProductSearch{Query: "已改名猫粮", Category: "宠物生活", Limit: 100})
	if err != nil || len(rows) != 1 || rows[0].ID != "short-b" {
		t.Fatal("projection did not follow administrator edit", ids(rows), err)
	}
	if err := db.Model(&domain.Record{}).Where("id = ?", "short-b").Update("status", "DISABLED").Error; err != nil {
		t.Fatal(err)
	}
	rows, err = r.SearchProducts(ctx, ProductSearch{Query: "已改名猫粮", Limit: 100})
	if err != nil || len(rows) != 0 {
		t.Fatal("projection did not follow disable", ids(rows), err)
	}
	if err := db.Exec("ALTER TABLE catalog_records DROP INDEX " + searchDocumentIndex + ", ADD INDEX " + searchDocumentIndex + " (id)").Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Migrate(ctx); err == nil {
		t.Fatal("migration trusted incorrect covering index")
	}
}

func TestMySQLSearchDocumentCoversThreeHundredThousandProducts(t *testing.T) {
	if os.Getenv("LUMO_MYSQL_SEARCH_SCALE") != "1" {
		t.Skip("requires explicitly enabled 300k disposable catalog benchmark")
	}
	ctx, db := mysqlSearchScratch(t)
	started := time.Now()
	digits := "(SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)"
	sequence := "SELECT a.n+10*b.n+100*c.n+1000*d.n+10000*e.n+100000*f.n AS n FROM " + digits + " a CROSS JOIN " + digits + " b CROSS JOIN " + digits + " c CROSS JOIN " + digits + " d CROSS JOIN " + digits + " e CROSS JOIN " + digits + " f"
	statement := "INSERT INTO catalog_records (id,user_id,parent_id,kind,status,payload,created_at,updated_at) SELECT CONCAT('load-',LPAD(n,6,'0')),'','','sku','ACTIVE',JSON_OBJECT('name',CONCAT('普通背景商品 ',n),'brand','Baseline','category','手机数码','price_cents',10000+n,'sales',n,'description',REPEAT('load payload ',100)),'2025-01-01','2025-01-01' FROM (" + sequence + ") AS source WHERE n < 300000"
	if err := db.Exec(statement).Error; err != nil {
		t.Fatal("populate isolated scale fixture", err)
	}
	t.Logf("populate_300k_ms=%d", time.Since(started).Milliseconds())
	for index, query := range []string{"养生壶", "猫粮", "冰箱"} {
		body, _ := json.Marshal(map[string]any{"name": "演示 " + query, "brand": "Sample", "category": "生活电器", "price_cents": 100 + index})
		row := domain.Record{ID: fmt.Sprintf("target-%d", index), Kind: "sku", Status: "ACTIVE", Payload: string(body), CreatedAt: time.Now()}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := New(db)
	started = time.Now()
	if err := r.Migrate(ctx); err != nil {
		t.Fatal("online projection/index backfill over 300k products", err)
	}
	t.Logf("online_migration_300k_ms=%d", time.Since(started).Milliseconds())
	for _, query := range []string{"养生壶", "猫粮", "冰箱", "完全不存在的词"} {
		started = time.Now()
		rows, err := r.SearchProducts(ctx, ProductSearch{Query: query, Limit: 100})
		if err != nil || query != "完全不存在的词" && len(rows) != 1 || query == "完全不存在的词" && len(rows) != 0 {
			t.Fatalf("indexed scale query %q failed in %v: rows=%d err=%v", query, time.Since(started), len(rows), err)
		}
		t.Logf("query=%q rows=%d elapsed_ms=%d", query, len(rows), time.Since(started).Milliseconds())
	}
	sql, args := projectedProductQuery(ProductSearch{Query: "养生壶", Limit: 100})
	var plan []struct {
		Table, Key, Extra string
		Rows              int64
	}
	if err := db.Raw("EXPLAIN "+sql, args...).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	covered := false
	for _, row := range plan {
		t.Logf("plan table=%s key=%s rows=%d extra=%s", row.Table, row.Key, row.Rows, row.Extra)
		if row.Key == searchDocumentIndex && strings.Contains(row.Extra, "Using index") {
			covered = true
		}
	}
	if !covered {
		t.Fatal("normal keyword branch does not use the covering index")
	}
}
