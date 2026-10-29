package repository

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/services/catalog/internal/domain"
	"gorm.io/gorm"
)

func TestMySQLListingUsesCoveringCountsAndOrderedAdminPages(t *testing.T) {
	ctx, db := mysqlSearchScratch(t)
	r := New(db)
	if err := r.migrateAdminListing(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.migrateAdminListing(ctx); err != nil {
		t.Fatal("repeat admin index migration", err)
	}
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
		if !tx.DryRun {
			queries = append(queries, query{sql: tx.Statement.SQL.String(), args: append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		state         string
		limit, offset int
		total         int64
		ids           []string
	}{
		{"ACTIVE", 1, 1, 3, []string{"b"}},
		{"DISABLED", 10, 0, 1, []string{"d"}},
		{"", 10, 0, 4, []string{"d", "c", "b", "a"}},
		{"", 2, 1, 4, []string{"c", "b"}},
		{"", 2, 100, 4, []string{}},
		{"ACTIVE", 1, 100, 3, []string{}},
	} {
		queries = nil
		rows, total, err := r.ListProducts(ctx, test.state, test.limit, test.offset)
		ids := []string{}
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if err != nil || total != test.total || !reflect.DeepEqual(ids, test.ids) {
			t.Fatalf(
				"listing changed for state=%q limit=%d offset=%d: %v/%d err=%v",
				test.state,
				test.limit,
				test.offset,
				ids,
				total,
				err,
			)
		}
		index := "idx_catalog_page"
		if test.state == "" {
			index = adminListingIndex
		}
		hint := "FORCE INDEX (" + index + ")"
		if len(queries) != 2 || !strings.Contains(queries[0].sql, hint) ||
			!strings.Contains(strings.ToLower(queries[0].sql), "count(") {
			t.Fatalf("count must use its covering index: %v", queries)
		}
		if test.state != "" && strings.Contains(queries[1].sql, "FORCE INDEX") {
			t.Fatalf("public page query must retain optimizer choice: %v", queries)
		}
		if test.state == "" && !strings.Contains(queries[1].sql, hint) {
			t.Fatalf("admin page must use its ordering index: %v", queries)
		}
		type planStep struct {
			SelectType string `gorm:"column:select_type"`
			Table      string
			Key        string
			Extra      string
			Type       string
			Rows       int64
		}
		var plan []planStep
		if err := db.Raw("EXPLAIN "+queries[0].sql, queries[0].args...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		if len(plan) != 1 || plan[0].Key != index || !strings.Contains(plan[0].Extra, "Using index") {
			t.Fatalf("count must use covering index: %v", plan)
		}
		t.Logf("state=%q count_total=%d count_plan=%+v", test.state, total, plan)
		if test.state == "" {
			plan = nil
			if err := db.Raw("EXPLAIN "+queries[1].sql, queries[1].args...).Scan(&plan).Error; err != nil {
				t.Fatal(err)
			}
			indexedPage, boundedPayload := false, false
			for _, step := range plan {
				if step.SelectType == "DERIVED" {
					indexedPage = step.Key == adminListingIndex && strings.Contains(step.Extra, "Using index") &&
						!strings.Contains(step.Extra, "filesort")
				}
				if step.Table == "product" {
					boundedPayload = step.Key == "PRIMARY" && step.Type == "eq_ref"
				}
				if strings.Contains(step.Extra, "filesort") && !strings.HasPrefix(step.Table, "<derived") {
					t.Fatalf("admin page sorted before selecting the bounded ID page: %+v", plan)
				}
			}
			if !indexedPage || !boundedPayload {
				t.Fatalf("admin page must select IDs before looking up product payloads: %+v", plan)
			}
			t.Logf("admin limit=%d offset=%d page_plan=%+v", test.limit, test.offset, plan)
		}
	}
}
