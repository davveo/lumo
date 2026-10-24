package checkout_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func configureCheckoutVariants(p *peers) {
	p.quoteItems = []platform.Result{
		{"sku_id": "sku-a", "quantity": 2, "unit_price_cents": 1000},
		{"sku_id": "sku-b", "quantity": 1, "unit_price_cents": 2000},
		{"sku_id": "sku-a", "quantity": 1, "unit_price_cents": 1000},
	}
	p.catalogRows = map[string]platform.Result{
		"sku-a": {"id": "sku-a", "active": true, "price_cents": 1000},
		"sku-b": {"id": "sku-b", "active": true, "price_cents": 2000},
	}
}

func checkoutBatchCommand(key string) platform.Command {
	return platform.Command{Action: "submit", UserID: "demo", Role: "user", IdempotencyKey: key, Data: map[string]any{"quote_id": "quote-fixed", "address": map[string]any{"recipient": "Batch Buyer", "phone": "13800000000", "address": "Batch Street"}}}
}

func TestCheckoutFreshCatalogBatchRetainsRevalidationAndIdempotentRetry(t *testing.T) {
	m, rpc := setup(t)
	configureCheckoutVariants(rpc)
	ctx := context.Background()
	first, err := m.Handle(ctx, checkoutBatchCommand("batch-first"))
	if err != nil {
		t.Fatal(err)
	}
	if rpc.catalogBatches != 1 || rpc.catalogGets != 0 || !reflect.DeepEqual(rpc.catalogIDs, []string{"sku-a", "sku-b"}) || rpc.catalogUser != "demo" || rpc.catalogRole != "user" {
		t.Fatalf("checkout did not make one identity-preserving unique batch: %#v", rpc)
	}
	if rpc.reserves != 1 {
		t.Fatalf("order did not reserve its resource once: %d", rpc.reserves)
	}
	retry, err := m.Handle(ctx, checkoutBatchCommand("batch-first"))
	if err != nil || first["order_id"] != retry["order_id"] || rpc.catalogBatches != 1 || rpc.reserves != 1 {
		t.Fatalf("idempotent retry repeated reads or resources: %v %v %v", first, retry, err)
	}
	rpc.catalogRows["sku-b"]["price_cents"] = 2001
	if _, err := m.Handle(ctx, checkoutBatchCommand("batch-new-attempt")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("new submit did not observe changed catalog price: %v", err)
	}
	if rpc.catalogBatches != 2 || rpc.reserves != 1 {
		t.Fatalf("catalog revalidation was cached or stale order reserved: batches=%d reserves=%d", rpc.catalogBatches, rpc.reserves)
	}
}

func TestCheckoutCatalogBatchFailsBeforeResourceMutations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*peers)
		code    codes.Code
	}{
		{"missing SKU", func(r *peers) { delete(r.catalogRows, "sku-b") }, codes.NotFound},
		{"inactive SKU", func(r *peers) { r.catalogRows["sku-b"]["active"] = false }, codes.FailedPrecondition},
		{"changed price", func(r *peers) { r.catalogRows["sku-b"]["price_cents"] = 999 }, codes.FailedPrecondition},
		{"catalog offline", func(r *peers) { r.catalogError = status.Error(codes.Unavailable, "catalog offline") }, codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, rpc := setup(t)
			configureCheckoutVariants(rpc)
			tc.prepare(rpc)
			if _, err := m.Handle(context.Background(), checkoutBatchCommand("batch-reject")); status.Code(err) != tc.code {
				t.Fatalf("wrong catalog validation result: %v", err)
			}
			if rpc.catalogBatches != 1 || rpc.catalogGets != 0 || rpc.reserves != 0 {
				t.Fatalf("failed batch reran per-SKU reads or reserved resources: batches=%d gets=%d reserves=%d", rpc.catalogBatches, rpc.catalogGets, rpc.reserves)
			}
		})
	}
}
