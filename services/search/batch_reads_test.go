package search_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	searchmod "github.com/davveo/lumo/services/search"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type searchBatchCaller struct {
	products      []platform.Result
	stockCalls    int
	requested     []string
	missing       string
	stockError    error
	lastStockUser string
	lastStockRole string
}

func (r *searchBatchCaller) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	if service == "catalog" && c.Action == "search_products" {
		if c.Role != "system" || c.UserID != "" {
			return nil, status.Error(codes.PermissionDenied, "catalog search must be an internal public read")
		}
		return platform.Result{"items": r.products}, nil
	}
	if service != "inventory" || c.Action != "batch_get" {
		return nil, status.Error(codes.Unimplemented, "unexpected search dependency")
	}
	r.stockCalls++
	r.lastStockUser, r.lastStockRole = c.UserID, c.Role
	var data struct {
		IDs []string `json:"ids"`
	}
	if err := platform.Decode(c.Data, &data); err != nil {
		return nil, err
	}
	r.requested = data.IDs
	if r.stockError != nil {
		return nil, r.stockError
	}
	items := []platform.Result{}
	for _, id := range data.IDs {
		if id == r.missing {
			continue
		}
		var index int
		_, _ = fmt.Sscanf(id, "sku-%03d", &index)
		available := int64(0)
		if index%2 == 0 {
			available = 5
		}
		items = append(items, platform.Result{"sku_id": id, "available": available})
	}
	return platform.Result{"items": items}, nil
}

func searchBatchSetup(t *testing.T) (platform.Module, *searchBatchCaller) {
	t.Helper()
	db, err := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	rpc := &searchBatchCaller{}
	for index := 0; index < 100; index++ {
		rpc.products = append(rpc.products, platform.Result{"id": fmt.Sprintf("sku-%03d", index), "name": "Phone", "brand": "LUMO", "category": "数码", "price_cents": int64(1000 + index)})
	}
	m := searchmod.New(platform.Dependencies{DB: db, RPC: rpc})
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m, rpc
}

func TestSearchBatchesInventoryAndPreservesFilteringOrderingAndPaging(t *testing.T) {
	m, rpc := searchBatchSetup(t)
	result, err := m.Handle(context.Background(), platform.Command{Action: "query", UserID: "buyer", Role: "user", Data: map[string]any{"query": "phone", "brand": "LUMO", "category": "数码", "in_stock": true, "sort": "price_desc", "offset": 1, "limit": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if rpc.stockCalls != 1 || len(rpc.requested) != 100 || rpc.lastStockUser != "buyer" || rpc.lastStockRole != "user" {
		t.Fatalf("not one identity-preserving inventory batch: %#v", rpc)
	}
	var rows struct {
		Items []map[string]any `json:"items"`
	}
	if err := platform.Decode(result, &rows); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, row := range rows.Items {
		ids = append(ids, platform.Text(row, "id"))
		if platform.Int(row, "available") != 5 {
			t.Fatalf("wrong stock projection: %v", row)
		}
	}
	if platform.Int(result, "total") != 50 || !reflect.DeepEqual(ids, []string{"sku-096", "sku-094", "sku-092"}) {
		t.Fatalf("batch altered search results: %v", result)
	}
	var snapshots struct {
		Items []map[string]any `json:"inventory_items"`
	}
	if err := platform.Decode(result, &snapshots); err != nil || len(snapshots.Items) != 3 {
		t.Fatalf("selected inventory snapshots missing: %v %v", result, err)
	}
	for i, row := range snapshots.Items {
		if platform.Text(row, "sku_id") != ids[i] || platform.Int(row, "available") != 5 {
			t.Fatalf("stock snapshot does not match selected product: %v", row)
		}
	}
}

func TestSearchAvoidsInventoryWhenNoCandidatesAndFailsClosedOnStockErrors(t *testing.T) {
	m, rpc := searchBatchSetup(t)
	_, err := m.Handle(context.Background(), platform.Command{Action: "query", Data: map[string]any{"query": "no-such-name"}})
	if err != nil || rpc.stockCalls != 0 {
		t.Fatalf("unmatched search read inventory: calls=%d error=%v", rpc.stockCalls, err)
	}
	rpc.missing = "sku-000"
	_, err = m.Handle(context.Background(), platform.Command{Action: "query"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing inventory silently accepted: %v", err)
	}
	rpc.missing, rpc.stockError = "", status.Error(codes.Unavailable, "inventory offline")
	_, err = m.Handle(context.Background(), platform.Command{Action: "query"})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("batch failure swallowed: %v", err)
	}
}
