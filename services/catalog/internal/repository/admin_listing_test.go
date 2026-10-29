package repository

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"gorm.io/gorm"
)

func TestAdminListingMigratesOrderedIndexAndPreservesAllStatuses(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	if err = db.AutoMigrate(&legacyCatalogRecord{}); err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []domain.Record{
		{ID: "a", Kind: "sku", Status: "ACTIVE", CreatedAt: first, Payload: `{"name":"A"}`},
		{ID: "b", Kind: "sku", Status: "DISABLED", CreatedAt: first.Add(time.Hour), Payload: `{"name":"B"}`},
		{ID: "c", Kind: "sku", Status: "ACTIVE", CreatedAt: first.Add(time.Hour), Payload: `{"name":"C"}`},
		{ID: "d", Kind: "sku", Status: "DISABLED", CreatedAt: first.Add(2 * time.Hour), Payload: `{"name":"D"}`},
		{ID: "child", ParentID: "a", Kind: "sku", Status: "ACTIVE", CreatedAt: first.Add(3 * time.Hour), Payload: `{}`},
		{
			ID:        "legacy",
			ParentID:  "__legacy__",
			Kind:      "sku",
			Status:    "ACTIVE",
			CreatedAt: first.Add(3 * time.Hour),
			Payload:   `{}`,
		},
	}
	if err = db.Create(&fixtures).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	if err = repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasIndex(&domain.Record{}, "idx_catalog_admin_page") {
		t.Fatal("admin listing has no index covering ordering across statuses")
	}
	type query struct {
		sql  string
		args []any
	}
	var queries []query
	if err = db.Callback().Query().After("gorm:query").Register("test:admin_listing_plan", func(tx *gorm.DB) {
		if tx.Statement.Table == "catalog_records" {
			queries = append(queries, query{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	rows, total, err := repo.ListProducts(ctx, "", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if total != 4 || !reflect.DeepEqual(ids, []string{"c", "b"}) {
		t.Fatalf("all-status pagination changed: ids=%v total=%d", ids, total)
	}
	if len(queries) != 2 {
		t.Fatalf("expected bounded count/page reads: %v", queries)
	}
	var plan []struct{ Detail string }
	if err = db.Raw("EXPLAIN QUERY PLAN "+queries[1].sql, queries[1].args...).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	details := ""
	for _, step := range plan {
		details += strings.ToUpper(step.Detail) + "\n"
	}
	if !strings.Contains(details, "IDX_CATALOG_ADMIN_PAGE") || strings.Contains(details, "TEMP B-TREE") {
		t.Fatalf("admin page still sorts the full directory: %s", details)
	}
	var stored []domain.Record
	if err = db.Order("id").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	want := map[string]domain.Record{}
	for _, row := range fixtures {
		want[row.ID] = row
	}
	if len(stored) != len(fixtures) {
		t.Fatal("migration changed catalog count")
	}
	for _, row := range stored {
		before := want[row.ID]
		if row.Payload != before.Payload || row.Status != before.Status || !row.CreatedAt.Equal(before.CreatedAt) {
			t.Fatalf("migration rewrote product %s", row.ID)
		}
	}
}
