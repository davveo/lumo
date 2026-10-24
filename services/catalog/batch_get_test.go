package catalog_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func TestCatalogBatchGetUsesOneSQLAndKeepsVisibilityRules(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { sql.Close() })
	if err = platform.MigratePlatform(ctx, db); err != nil {
		t.Fatal(err)
	}
	module := catalog.New(platform.Dependencies{DB: db})
	if err = module.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"active", "disabled"} {
		_, err = module.Handle(ctx, platform.Command{Action: "upsert", ID: id, UserID: "admin", Role: "admin", Data: map[string]any{"name": id, "price_cents": 1234, "active": id == "active"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	queries := 0
	if err = db.Callback().Query().Before("gorm:query").Register("catalog-batch-query-count", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	command := platform.Command{Action: "batch_get", Data: map[string]any{"ids": []string{"disabled", "active", "missing", "active"}}}
	out, err := module.Handle(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	items := out["items"].([]platform.Result)
	missing := out["missing_ids"].([]string)
	if queries != 1 || len(items) != 1 || platform.Text(items[0], "id") != "active" || platform.Int(items[0], "price_cents") != 1234 || len(missing) != 2 || missing[0] != "disabled" || missing[1] != "missing" {
		t.Fatalf("public visibility/order or query count wrong: queries=%d rows=%v", queries, out)
	}
	queries = 0
	command.UserID, command.Role = "admin", "admin"
	out, err = module.Handle(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	items = out["items"].([]platform.Result)
	if queries != 1 || len(items) != 2 || platform.Text(items[0], "id") != "disabled" || items[0]["active"] != false {
		t.Fatalf("admin batch changed get semantics: %v", out)
	}
	for _, ids := range [][]string{nil, {""}, {" active"}, make([]string, 501)} {
		command.Data = map[string]any{"ids": ids}
		_, err = module.Handle(ctx, command)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatal("invalid ids accepted")
		}
	}
}
