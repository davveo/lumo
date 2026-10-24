package recommendation_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	recommendationmod "github.com/davveo/lumo/services/recommendation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type recommendationBatchCaller struct {
	products   []platform.Result
	stockCalls int
	requested  []string
	missing    string
	stockError error
}

func (r *recommendationBatchCaller) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	if service == "catalog" && c.Action == "list" {
		return platform.Result{"items": r.products}, nil
	}
	if service == "order" && c.Action == "get" {
		return platform.Result{"user_id": "buyer", "items": []platform.Result{{"sku_id": "sku-000"}}}, nil
	}
	if service != "inventory" || c.Action != "batch_get" {
		return nil, status.Error(codes.Unimplemented, "unexpected recommendation dependency")
	}
	r.stockCalls++
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
			available = 3
		}
		items = append(items, platform.Result{"sku_id": id, "available": available})
	}
	return platform.Result{"items": items}, nil
}

func recommendationBatchSetup(t *testing.T) (platform.Module, *recommendationBatchCaller) {
	t.Helper()
	db, err := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "recommendation.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	rpc := &recommendationBatchCaller{}
	for index := 0; index < 100; index++ {
		rpc.products = append(rpc.products, platform.Result{"id": fmt.Sprintf("sku-%03d", index), "name": "Phone"})
	}
	m := recommendationmod.New(platform.Dependencies{DB: db, RPC: rpc})
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m, rpc
}

func TestRecommendationsUseOneStockBatchAndPreservePurchasedExclusion(t *testing.T) {
	m, rpc := recommendationBatchSetup(t)
	result, err := m.Handle(context.Background(), platform.Command{Action: "list", UserID: "buyer", Role: "user", Data: map[string]any{"order_id": "order-one", "limit": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if rpc.stockCalls != 1 || len(rpc.requested) != 99 {
		t.Fatalf("not one deduplicated inventory batch: %#v", rpc)
	}
	for _, id := range rpc.requested {
		if id == "sku-000" {
			t.Fatal("purchased SKU was read for recommendation")
		}
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
	}
	if !reflect.DeepEqual(ids, []string{"sku-002", "sku-004", "sku-006"}) {
		t.Fatalf("batch changed availability/order/exclusion: %v", result)
	}
}

func TestRecommendationBatchPreservesFailureAndEarlyLimitSemantics(t *testing.T) {
	m, rpc := recommendationBatchSetup(t)
	rpc.missing = "sku-099"
	if _, err := m.Handle(context.Background(), platform.Command{Action: "list", Data: map[string]any{"limit": 1}}); err != nil {
		t.Fatalf("unneeded later missing row failed first recommendation: %v", err)
	}
	rpc.missing = "sku-000"
	if result, err := m.Handle(context.Background(), platform.Command{Action: "list"}); err != nil || result["degraded"] != true {
		t.Fatalf("missing candidate must be skipped with degradation: result=%v error=%v", result, err)
	}
	rpc.missing, rpc.stockError = "", status.Error(codes.Unavailable, "inventory offline")
	if _, err := m.Handle(context.Background(), platform.Command{Action: "list"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("batch outage swallowed: %v", err)
	}
	rpc.stockError, rpc.products, rpc.stockCalls = nil, nil, 0
	if _, err := m.Handle(context.Background(), platform.Command{Action: "list"}); err != nil || rpc.stockCalls != 0 {
		t.Fatalf("empty catalog read stock: %v calls=%d", err, rpc.stockCalls)
	}
}
