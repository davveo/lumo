package catalog_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func searchProducts(t *testing.T, module platform.Module, data map[string]any) []platform.Result {
	t.Helper()
	result, err := module.Handle(context.Background(), platform.Command{Action: "search_products", Role: "system", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return result["items"].([]platform.Result)
}

func TestProductSearchFiltersEntireCatalogBeforeBoundedPage(t *testing.T) {
	m, db := recallSetup(t)
	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	now := old.AddDate(2, 0, 0)
	for i := range 125 {
		insertRecallProduct(t, db, fmt.Sprintf("recent-%03d", i), "常见无线耳机", "Common", "手机数码", "ACTIVE", "", true, now.Add(time.Duration(i)*time.Minute))
	}
	insertRecallProduct(t, db, "older-kettle", "恒温养生壶", "Quiet Studio", "生活电器", "ACTIVE", "", true, old)
	insertRecallProduct(t, db, "other-category", "恒温养生壶", "Quiet Studio", "厨具餐具", "ACTIVE", "", true, now)
	insertRecallProduct(t, db, "other-brand", "恒温养生壶", "Other", "生活电器", "ACTIVE", "", true, now)
	insertRecallProduct(t, db, "disabled", "恒温养生壶", "Quiet Studio", "生活电器", "DISABLED", "", true, now)
	insertRecallProduct(t, db, "child", "恒温养生壶", "Quiet Studio", "生活电器", "ACTIVE", "older-kettle", true, now)
	insertRecallProduct(t, db, "hidden", "恒温养生壶", "Quiet Studio", "生活电器", "ACTIVE", "", false, now)
	if err := db.Table("catalog_records").Create(map[string]any{"id": "broken", "kind": "sku", "parent_id": "", "status": "ACTIVE", "payload": "not JSON", "created_at": now, "updated_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	var queries atomic.Int64
	_ = db.Callback().Query().Before("gorm:query").Register("count-catalog-search", func(tx *gorm.DB) {
		if !tx.DryRun {
			queries.Add(1)
		}
	})
	rows := searchProducts(t, m, map[string]any{"query": "养生壶", "category": "生活电器", "brand": "Quiet Studio", "limit": 100})
	if queries.Load() != 1 || len(rows) != 1 || platform.Text(rows[0], "id") != "older-kettle" {
		t.Fatalf("full-catalog AND constraints were not applied in one query: queries=%d rows=%v", queries.Load(), rows)
	}
	if platform.Text(rows[0], "image") != "product.png" || rows[0]["specs"] == nil || platform.Int(rows[0], "price_cents") != 12345 {
		t.Fatalf("full product metadata lost: %v", rows)
	}
	rows = searchProducts(t, m, map[string]any{"category": "生活电器"})
	if len(rows) != 2 {
		t.Fatalf("empty keyword must browse all matching category products: %v", rows)
	}
	rows = searchProducts(t, m, map[string]any{"query": "无线耳机", "category": "生活电器"})
	if len(rows) != 0 {
		t.Fatal("keyword and category must be combined with AND", rows)
	}
}

func TestProductSearchSortsAcrossCatalogBeforeTakingLimit(t *testing.T) {
	m, db := recallSetup(t)
	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 120 {
		insertRecallProduct(t, db, fmt.Sprintf("new-%03d", i), "Product", "Brand", "电脑办公", "ACTIVE", "", true, old.AddDate(2, 0, 0).Add(time.Duration(i)*time.Minute))
	}
	insertRecallProduct(t, db, "old-cheap", "Old Affordable", "Brand", "电脑办公", "ACTIVE", "", true, old)
	insertRecallProduct(t, db, "old-expensive", "Old Premium", "Brand", "电脑办公", "ACTIVE", "", true, old)
	if err := db.Table("catalog_records").Where("id = ?", "old-cheap").Update("payload", `{"name":"Old Affordable","category":"电脑办公","price_cents":100,"sales":99999}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("catalog_records").Where("id = ?", "old-expensive").Update("payload", `{"name":"Old Premium","category":"电脑办公","price_cents":9000000,"sales":1}`).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ sort, id string }{{"price_asc", "old-cheap"}, {"price_desc", "old-expensive"}, {"sales", "old-cheap"}, {"newest", "new-119"}} {
		rows := searchProducts(t, m, map[string]any{"category": "电脑办公", "sort": tc.sort, "limit": 1})
		if len(rows) != 1 || platform.Text(rows[0], "id") != tc.id {
			t.Fatalf("sort %s only reordered a truncated catalog: %v", tc.sort, rows)
		}
	}
}

func TestProductSearchTreatsQueryAndFiltersAsLiteralData(t *testing.T) {
	m, db := recallSetup(t)
	now := time.Now()
	insertRecallProduct(t, db, "literal", "Studio 100%_! Cotton", "Brand' OR 1=1 --", "家居生活", "ACTIVE", "", true, now)
	insertRecallProduct(t, db, "other", "Studio Other Cotton", "Other", "家居生活", "ACTIVE", "", true, now)
	for _, data := range []map[string]any{{"query": "100%_!"}, {"brand": "Brand' OR 1=1 --"}} {
		rows := searchProducts(t, m, data)
		if len(rows) != 1 || platform.Text(rows[0], "id") != "literal" {
			t.Fatalf("query became wildcard or SQL logic: %v %v", data, rows)
		}
	}
	for _, data := range []map[string]any{{"query": "missing%' OR 1=1 --"}, {"category": "家居生活' OR 1=1 --"}} {
		if rows := searchProducts(t, m, data); len(rows) != 0 {
			t.Fatalf("injection returned unrelated products: %v %v", data, rows)
		}
	}
}

func TestProductSearchValidatesInternalRoleAndBoundsBeforeSQL(t *testing.T) {
	m, db := recallSetup(t)
	queries := 0
	_ = db.Callback().Query().Before("gorm:query").Register("count-invalid-search", func(*gorm.DB) { queries++ })
	for _, role := range []string{"", "user", "admin"} {
		_, err := m.Handle(context.Background(), platform.Command{Action: "search_products", Role: role, Data: map[string]any{}})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("non-system search allowed: %q %v", role, err)
		}
	}
	for _, data := range []map[string]any{{"limit": -1}, {"limit": 101}, {"limit": 1.5}, {"query": strings.Repeat("长", 81)}, {"brand": strings.Repeat("a", 65)}, {"category": "bad\x00"}, {"query": []string{"a"}}, {"sort": "price_cents; DROP TABLE catalog_records"}, {"user_id": "other"}} {
		_, err := m.Handle(context.Background(), platform.Command{Action: "search_products", Role: "system", Data: data})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid search accepted: %v %v", data, err)
		}
	}
	if queries != 0 {
		t.Fatalf("invalid search touched SQL: %d", queries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.Handle(ctx, platform.Command{Action: "search_products", Role: "system"})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("search ignored cancellation: %v", err)
	}
}
