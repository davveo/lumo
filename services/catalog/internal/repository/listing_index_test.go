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

// This is the pre-index schema, so the migration test starts with real stored
// records and the existing single-column indexes rather than an empty table.
type legacyCatalogRecord struct {
	ID        string `gorm:"primaryKey;size:128"`
	UserID    string `gorm:"size:128;index"`
	ParentID  string `gorm:"size:128;index"`
	Kind      string `gorm:"size:32;index"`
	Status    string `gorm:"size:32;index"`
	Payload   string `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (legacyCatalogRecord) TableName() string { return "catalog_records" }

func TestListingIndexMigratesExistingCatalogAndCoversCount(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if err := db.AutoMigrate(&legacyCatalogRecord{}); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasIndex(&domain.Record{}, "idx_catalog_page") {
		t.Fatal("legacy schema unexpectedly has the composite listing index")
	}
	first := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []domain.Record{
		{ID: "product-a", Kind: "sku", Status: "ACTIVE", CreatedAt: first},
		{ID: "product-b", Kind: "sku", Status: "ACTIVE", CreatedAt: first.Add(time.Hour)},
		{ID: "product-c", Kind: "sku", Status: "ACTIVE", CreatedAt: first.Add(time.Hour)},
		{ID: "product-disabled", Kind: "sku", Status: "DISABLED", CreatedAt: first.Add(2 * time.Hour)},
		{
			ID:        "product-child",
			ParentID:  "product-a",
			Kind:      "sku",
			Status:    "ACTIVE",
			CreatedAt: first.Add(3 * time.Hour),
		},
		{ID: "sku-legacy", ParentID: "__legacy__", Kind: "sku", Status: "ACTIVE", CreatedAt: first.Add(4 * time.Hour)},
		{ID: "catalog-policy", Kind: "policy", Status: "ACTIVE", CreatedAt: first.Add(5 * time.Hour)},
	}
	for i := range fixtures {
		fixtures[i].Payload = `{"marker":"` + fixtures[i].ID + `"}`
	}
	if err := db.Create(&fixtures).Error; err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	before, totalBefore, err := repository.ListProducts(ctx, "ACTIVE", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// A second startup must reuse the same index and preserve all existing data.
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{
		"idx_catalog_page",
		"idx_catalog_records_user_id",
		"idx_catalog_records_parent_id",
		"idx_catalog_records_kind",
		"idx_catalog_records_status",
	} {
		if !db.Migrator().HasIndex(&domain.Record{}, index) {
			t.Errorf("migration did not retain/create %s", index)
		}
	}
	var indexFields []struct {
		Name string
		Desc int
		Key  int
	}
	if err := db.Raw("PRAGMA index_xinfo('idx_catalog_page')").Scan(&indexFields).Error; err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, field := range indexFields {
		if field.Key == 1 {
			names = append(names, field.Name)
			if field.Desc != 0 {
				t.Errorf("existing ascending index column %s changed direction", field.Name)
			}
		}
	}
	if !reflect.DeepEqual(names, []string{"kind", "parent_id", "status", "created_at", "id"}) {
		t.Fatalf("listing index is incompatible with the existing MySQL index: %v", names)
	}
	var persisted []domain.Record
	if err := db.Order("id").Find(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if len(persisted) != len(fixtures) {
		t.Fatal("migration changed the number of catalog records")
	}
	for _, record := range persisted {
		if record.Payload != `{"marker":"`+record.ID+`"}` {
			t.Errorf("migration changed payload of %s", record.ID)
		}
	}

	type query struct {
		sql  string
		args []any
	}
	var queries []query
	if err := db.Callback().Query().After("gorm:query").Register("test:listing_plan", func(tx *gorm.DB) {
		if tx.Statement.Table == "catalog_records" {
			queries = append(
				queries,
				query{sql: tx.Statement.SQL.String(), args: append([]any(nil), tx.Statement.Vars...)},
			)
		}
	}); err != nil {
		t.Fatal(err)
	}
	after, totalAfter, err := repository.ListProducts(ctx, "ACTIVE", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if totalBefore != 3 || totalAfter != totalBefore || len(after) != 1 || after[0].ID != "product-b" ||
		!reflect.DeepEqual(before, after) {
		t.Fatalf("public pagination changed: before=%v/%d after=%v/%d", before, totalBefore, after, totalAfter)
	}
	if len(queries) != 2 {
		t.Fatalf("expected count and page queries, got %v", queries)
	}
	for i, query := range queries {
		var plan []struct{ Detail string }
		if err := db.Raw("EXPLAIN QUERY PLAN "+query.sql, query.args...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		var details []string
		for _, step := range plan {
			details = append(details, strings.ToUpper(step.Detail))
		}
		joined := strings.Join(details, "\n")
		if !strings.Contains(joined, "INDEX IDX_CATALOG_PAGE") {
			t.Errorf("listing query does not use the composite index: %s\n%s", query.sql, joined)
		}
		if i == 0 && !strings.Contains(joined, "COVERING INDEX IDX_CATALOG_PAGE") {
			t.Errorf("count query still needs table lookups: %s", joined)
		}
		if i == 1 && strings.Contains(joined, "TEMP B-TREE") {
			t.Errorf("page ordering still needs a temporary sort: %s", joined)
		}
	}
	all, total, err := repository.ListProducts(ctx, "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, record := range all {
		ids = append(ids, record.ID)
	}
	if total != 4 || !reflect.DeepEqual(ids, []string{"product-disabled", "product-c", "product-b", "product-a"}) {
		t.Fatalf("admin listing must include disabled roots and exclude children/legacy/non-SKUs: %v/%d", ids, total)
	}
}
