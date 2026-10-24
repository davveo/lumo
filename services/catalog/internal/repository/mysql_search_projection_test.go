package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
)

// Explicit opt-in only: run this binary inside the local MySQL container. It
// creates and drops a fresh scratch schema and never opens lumo_catalog.
func TestMySQLCategoryProjectionOnlineMigration(t *testing.T) {
	if os.Getenv("LUMO_MYSQL_PROJECTION_TEST") != "1" {
		t.Skip("requires explicitly enabled local MySQL scratch-schema test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	password := os.Getenv("MYSQL_ROOT_PASSWORD")
	if password == "" {
		t.Fatal("local MySQL test credential missing")
	}
	admin, err := platform.OpenDB(
		ctx,
		"mysql",
		"root:"+password+"@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC",
	)
	if err != nil {
		t.Fatal("connect scratch-schema administrator", err)
	}
	rawAdmin, _ := admin.DB()
	defer rawAdmin.Close()
	name := fmt.Sprintf("lumo_category_projection_probe_%d", time.Now().UnixNano())
	if err = admin.Exec(
		"CREATE DATABASE " + name + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
	).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := admin.Exec("DROP DATABASE " + name).Error; err != nil {
			t.Error("cleanup disposable schema", err)
		}
	}()
	db, err := platform.OpenDB(
		ctx,
		"mysql",
		"root:"+password+"@tcp(127.0.0.1:3306)/"+name+"?charset=utf8mb4&parseTime=true&loc=UTC",
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	defer raw.Close()
	r := New(db)
	if err = r.Migrate(ctx); err != nil {
		t.Fatal("first online migration", err)
	}
	if err = r.Migrate(ctx); err != nil {
		t.Fatal("idempotent online migration", err)
	}
	if !r.searchCategoryIndexed {
		t.Fatal("verified projection index was not enabled")
	}
	if err = db.Exec(
		"ALTER TABLE catalog_records ADD COLUMN category VARCHAR(80) DEFAULT 'wrong ordinary column'",
	).Error; err != nil {
		t.Fatal(err)
	}
	row := domain.Record{
		ID:      "product",
		Kind:    "sku",
		Status:  "ACTIVE",
		Payload: `{"name":"恒温养生壶","category":"生活电器","price_cents":1000}`,
	}
	if err = r.Save(ctx, &row, nil); err != nil {
		t.Fatal("normal write must not write generated column", err)
	}
	rows, err := r.SearchProducts(ctx, ProductSearch{Category: "生活电器", Limit: 100})
	if err != nil || len(rows) != 1 || rows[0].ID != "product" {
		t.Fatalf("ordinary stale category contaminated search: %v %v", rows, err)
	}
	row.Payload = `{"name":"恒温养生壶","category":"厨具餐具","price_cents":1000}`
	if err = r.Save(ctx, &row, nil); err != nil {
		t.Fatal(err)
	}
	rows, err = r.SearchProducts(ctx, ProductSearch{Category: "生活电器", Limit: 100})
	if err != nil || len(rows) != 0 {
		t.Fatal("generated projection did not track updated payload", rows, err)
	}
	rows, err = r.SearchProducts(ctx, ProductSearch{Category: "厨具餐具", Limit: 100})
	if err != nil || len(rows) != 1 {
		t.Fatal("updated category was not searchable", rows, err)
	}
	if err = db.Exec("ALTER TABLE catalog_records DROP INDEX " + searchCategoryIndex).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("ALTER TABLE catalog_records ADD INDEX " + searchCategoryIndex + " (id)").Error; err != nil {
		t.Fatal(err)
	}
	if err = r.Migrate(ctx); err == nil {
		t.Fatal("migration silently trusted incorrect index")
	}
	if err = db.Exec(
		"ALTER TABLE catalog_records DROP INDEX " + searchCategoryIndex + ", DROP COLUMN search_category_v1",
	).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("ALTER TABLE catalog_records ADD COLUMN search_category_v1 VARCHAR(80)").Error; err != nil {
		t.Fatal(err)
	}
	if err = r.Migrate(ctx); err == nil {
		t.Fatal("migration silently trusted an ordinary projection column")
	}
}
