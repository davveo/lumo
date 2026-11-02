package tests_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog"
	"github.com/davveo/lumo/services/inventory"
	"github.com/davveo/lumo/services/search"
)

type paginationRouter map[string]platform.Module

type countedPaginationRouter struct {
	router             paginationRouter
	catalog, inventory int
}

func (r *countedPaginationRouter) Call(ctx context.Context, name string, c platform.Command) (platform.Result, error) {
	if name == "catalog" {
		r.catalog++
	}
	if name == "inventory" {
		r.inventory++
	}
	return r.router.Call(ctx, name, c)
}

func (r paginationRouter) Call(ctx context.Context, name string, c platform.Command) (platform.Result, error) {
	return r[name].Handle(ctx, c)
}

func TestSearchPaginatesPastCandidateWindowAndFindsStock(t *testing.T) {
	ctx := context.Background()
	router := paginationRouter{}
	peers := &countedPaginationRouter{router: router}
	constructors := map[string]func(platform.Dependencies) platform.Module{
		"catalog":   catalog.New,
		"inventory": inventory.New,
		"search":    search.New,
	}
	for name, create := range constructors {
		db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), name+".db"))
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := db.DB()
		t.Cleanup(func() { raw.Close() })
		if e = platform.MigratePlatform(ctx, db); e != nil {
			t.Fatal(e)
		}
		module := create(platform.Dependencies{DB: db, RPC: peers})
		if e = module.Migrate(ctx); e != nil {
			t.Fatal(e)
		}
		router[name] = module
	}
	call := func(name string, c platform.Command) platform.Result {
		t.Helper()
		catalogBefore, inventoryBefore := peers.catalog, peers.inventory
		r, e := router.Call(ctx, name, c)
		if e != nil {
			t.Fatal(e)
		}
		if name == "search" && c.Action == "query" &&
			(peers.catalog-catalogBefore != 1 || peers.inventory-inventoryBefore > 1) {
			t.Fatalf(
				"search RPC count grows with candidates: catalog=%d inventory=%d",
				peers.catalog-catalogBefore,
				peers.inventory-inventoryBefore,
			)
		}
		return r
	}
	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("review-sku-%03d", i)
		call(
			"catalog",
			platform.Command{
				Action: "upsert",
				ID:     id,
				UserID: "review-admin",
				Role:   "admin",
				Data: map[string]any{
					"name":        "Review Phone",
					"price_cents": 1000 + i,
					"brand":       "Review",
					"category":    "Review",
					"active":      true,
				},
			},
		)
		stock := 0
		if i == 100 {
			stock = 1
		}
		call(
			"inventory",
			platform.Command{
				Action: "adjust",
				ID:     id,
				UserID: "review-admin",
				Role:   "admin",
				Data:   map[string]any{"on_hand": stock},
			},
		)
	}
	first := call(
		"search",
		platform.Command{
			Action: "query",
			Data:   map[string]any{"query": "Review Phone", "sort": "price_asc", "limit": 100},
		},
	)
	var rows struct {
		Items []platform.Result `json:"items"`
	}
	if e := platform.Decode(first, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows.Items) != 100 || first["has_more"] != true || first["total"] != nil {
		t.Fatalf("first page claimed a truncated total: %v", first)
	}
	second := call(
		"search",
		platform.Command{
			Action: "query",
			Data: map[string]any{
				"query":  "Review Phone",
				"sort":   "price_asc",
				"limit":  100,
				"cursor": first["next_cursor"],
			},
		},
	)
	if e := platform.Decode(second, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows.Items) != 1 || rows.Items[0]["id"] != "review-sku-100" || platform.Int(second, "total") != 101 ||
		second["has_more"] != false {
		t.Fatal("second page lost matching products", second)
	}
	inStock := call(
		"search",
		platform.Command{
			Action: "query",
			Data:   map[string]any{"query": "Review Phone", "sort": "price_asc", "in_stock": true, "limit": 100},
		},
	)
	if e := platform.Decode(inStock, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows.Items) != 0 || inStock["has_more"] != true || inStock["total_exact"] != false ||
		platform.Text(inStock, "next_cursor") == "" {
		t.Fatal("sparse candidate page lost its continuation", inStock)
	}
	lastStock := call(
		"search",
		platform.Command{
			Action: "query",
			Data: map[string]any{
				"query":    "Review Phone",
				"sort":     "price_asc",
				"in_stock": true,
				"limit":    100,
				"cursor":   inStock["next_cursor"],
			},
		},
	)
	if e := platform.Decode(lastStock, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows.Items) != 1 || rows.Items[0]["id"] != "review-sku-100" || platform.Int(lastStock, "total") != 1 ||
		lastStock["has_more"] != false {
		t.Fatal("stock continuation lost matching products", lastStock)
	}
	_, err := router.Call(
		ctx,
		"search",
		platform.Command{
			Action: "query",
			Data:   map[string]any{"query": "different query", "cursor": first["next_cursor"]},
		},
	)
	if err == nil {
		t.Fatal("cursor accepted for another query")
	}
	call(
		"search",
		platform.Command{
			Action: "policy_upsert",
			UserID: "admin",
			Role:   "admin",
			Data: map[string]any{
				"version": 0,
				"reason":  "Exercise sparse search continuations",
				"config":  map[string]any{"max_results": 1},
			},
		},
	)
	cursor := ""
	found := false
	for i := 0; i < 101; i++ {
		page := call(
			"search",
			platform.Command{
				Action: "query",
				Data: map[string]any{
					"query":    "Review Phone",
					"sort":     "price_asc",
					"in_stock": true,
					"limit":    100,
					"cursor":   cursor,
				},
			},
		)
		if e := platform.Decode(page, &rows); e != nil {
			t.Fatal(e)
		}
		if len(rows.Items) > 0 {
			found = rows.Items[0]["id"] == "review-sku-100"
			break
		}
		next := platform.Text(page, "next_cursor")
		if page["has_more"] != true || page["total"] != nil || next == "" || next == cursor {
			t.Fatal("empty partial page pretends search is finished", page)
		}
		cursor = next
	}
	if !found {
		t.Fatal("sparse search continuation never reached the in-stock product")
	}
}
