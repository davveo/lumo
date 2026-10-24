package repository

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/services/catalog/internal/domain"
	"gorm.io/gorm"
)

func TestMySQLListingCountUsesCoveringIndexWithoutChangingPageQuery(t *testing.T) {
	ctx, db := mysqlSearchScratch(t)
	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []domain.Record{
		{ID: "a", Kind: "sku", Status: "ACTIVE", CreatedAt: created},
		{ID: "b", Kind: "sku", Status: "ACTIVE", CreatedAt: created.Add(time.Hour)},
		{ID: "c", Kind: "sku", Status: "ACTIVE", CreatedAt: created.Add(time.Hour)},
		{ID: "d", Kind: "sku", Status: "DISABLED", CreatedAt: created.Add(2 * time.Hour)},
		{ID: "child", Kind: "sku", Status: "ACTIVE", ParentID: "a", CreatedAt: created},
		{ID: "legacy", Kind: "sku", Status: "ACTIVE", ParentID: "__legacy__", CreatedAt: created},
		{ID: "policy", Kind: "policy", Status: "ACTIVE", CreatedAt: created},
	}
	for index := range fixtures {
		fixtures[index].Payload = `{"name":"fixture"}`
	}
	if err := db.Create(&fixtures).Error; err != nil {
		t.Fatal(err)
	}
	type query struct {
		sql  string
		args []any
	}
	queries := []query{}
	if err := db.Callback().Query().After("gorm:query").Register("test:mysql_listing_count", func(tx *gorm.DB) {
		queries = append(queries, query{sql: tx.Statement.SQL.String(), args: append([]any(nil), tx.Statement.Vars...)})
	}); err != nil {
		t.Fatal(err)
	}
	r := New(db)
	for _, test := range []struct {
		state         string
		limit, offset int
		total         int64
		ids           []string
	}{
		{"ACTIVE", 1, 1, 3, []string{"b"}},
		{"DISABLED", 10, 0, 1, []string{"d"}},
		{"", 10, 0, 4, []string{"d", "c", "b", "a"}},
		{"ACTIVE", 1, 100, 3, []string{}},
	} {
		queries = nil
		rows, total, err := r.ListProducts(ctx, test.state, test.limit, test.offset)
		ids := []string{}
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if err != nil || total != test.total || !reflect.DeepEqual(ids, test.ids) {
			t.Fatalf("listing changed for state=%q limit=%d offset=%d: %v/%d err=%v", test.state, test.limit, test.offset, ids, total, err)
		}
		if len(queries) != 2 || !strings.Contains(queries[0].sql, "FORCE INDEX (idx_catalog_page)") || !strings.Contains(strings.ToLower(queries[0].sql), "count(") || strings.Contains(queries[1].sql, "FORCE INDEX") {
			t.Fatalf("hint must be confined to count, keeping page query untouched: %v", queries)
		}
		var plan []struct{ Key, Extra string }
		if err := db.Raw("EXPLAIN "+queries[0].sql, queries[0].args...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		if len(plan) != 1 || plan[0].Key != "idx_catalog_page" || !strings.Contains(plan[0].Extra, "Using index") {
			t.Fatalf("count must use existing covering index: %v", plan)
		}
		t.Logf("state=%q count_total=%d count_plan=%+v page_without_hint=true", test.state, total, plan)
	}
}
