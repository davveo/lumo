package pricing_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	pricingmod "github.com/davveo/lumo/services/pricing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type quoteBatchCaller struct {
	rows       map[string]platform.Result
	batches    int
	individual int
	ids        []string
	user, role string
	failure    error
}

func (r *quoteBatchCaller) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	if service == "campaign" {
		return platform.Result{"eligible": true, "discount_cents": int64(0)}, nil
	}
	if service != "catalog" {
		return nil, status.Error(codes.Unimplemented, "unexpected quote dependency")
	}
	if c.Action != "batch_get" {
		r.individual++
		return nil, status.Error(codes.Internal, "individual catalog read")
	}
	r.batches++
	r.user, r.role = c.UserID, c.Role
	var data struct {
		IDs []string `json:"ids"`
	}
	if err := platform.Decode(c.Data, &data); err != nil {
		return nil, err
	}
	r.ids = data.IDs
	if r.failure != nil {
		return nil, r.failure
	}
	items, missing := []platform.Result{}, []string{}
	for _, id := range data.IDs {
		if row, ok := r.rows[id]; ok {
			items = append(items, row)
		} else {
			missing = append(missing, id)
		}
	}
	return platform.Result{"items": items, "missing_ids": missing}, nil
}

func quoteBatchSetup(t *testing.T) (platform.Module, *quoteBatchCaller) {
	t.Helper()
	db, err := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "quotes.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	rpc := &quoteBatchCaller{rows: map[string]platform.Result{
		"sku-a": {"id": "sku-a", "name": "Variant A", "active": true, "price_cents": int64(101)},
		"sku-b": {"id": "sku-b", "name": "Variant B", "active": true, "price_cents": int64(205)},
	}}
	m := pricingmod.New(platform.Dependencies{DB: db, RPC: rpc})
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m, rpc
}

func batchQuoteCommand() platform.Command {
	return platform.Command{Action: "quote", UserID: "buyer", Role: "user", Data: map[string]any{"items": []platform.Result{
		{"sku_id": "sku-a", "quantity": 2, "unit_price_cents": 1},
		{"sku_id": "sku-b", "quantity": 3, "unit_price_cents": 1},
		{"sku_id": "sku-a", "quantity": 1, "unit_price_cents": 1},
	}}}
}

func TestQuoteFreshCatalogBatchPreservesVariantsAndImmutablePriceSnapshot(t *testing.T) {
	m, rpc := quoteBatchSetup(t)
	ctx := context.Background()
	q, err := m.Handle(ctx, batchQuoteCommand())
	if err != nil {
		t.Fatal(err)
	}
	if rpc.batches != 1 || rpc.individual != 0 || !reflect.DeepEqual(rpc.ids, []string{"sku-a", "sku-b"}) || rpc.user != "buyer" || rpc.role != "user" {
		t.Fatalf("catalog reads were not batched with correct identity: %#v", rpc)
	}
	if platform.Int(q, "subtotal_cents") != 918 || platform.Int(q, "payable_cents") != 918 {
		t.Fatalf("batch lost actual variant prices: %v", q)
	}
	var detail struct {
		Items []map[string]any `json:"items"`
	}
	if err := platform.Decode(q, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Items) != 3 || platform.Int(detail.Items[0], "amount_cents") != 202 || platform.Int(detail.Items[1], "unit_price_cents") != 205 || platform.Text(detail.Items[2], "name") != "Variant A" {
		t.Fatalf("quoted item snapshots changed: %v", q)
	}
	rpc.rows["sku-a"]["price_cents"] = int64(1000)
	stored, err := m.Handle(ctx, platform.Command{Action: "get", ID: platform.Text(q, "quote_id"), UserID: "buyer", Role: "user"})
	if err != nil || platform.Int(stored, "subtotal_cents") != 918 || rpc.batches != 1 {
		t.Fatalf("stored quote reread current catalog: %v %v", stored, err)
	}
	fresh, err := m.Handle(ctx, batchQuoteCommand())
	if err != nil || platform.Int(fresh, "subtotal_cents") != 3615 || rpc.batches != 2 {
		t.Fatalf("new quote reused cached catalog: %v %v calls=%d", fresh, err, rpc.batches)
	}
}

func TestQuoteCatalogBatchRejectsMissingInactiveAndUnavailableDependencies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*quoteBatchCaller)
		code    codes.Code
	}{
		{"missing", func(r *quoteBatchCaller) { delete(r.rows, "sku-b") }, codes.NotFound},
		{"inactive", func(r *quoteBatchCaller) { r.rows["sku-b"]["active"] = false }, codes.FailedPrecondition},
		{"invalid price", func(r *quoteBatchCaller) { r.rows["sku-b"]["price_cents"] = int64(0) }, codes.FailedPrecondition},
		{"unavailable", func(r *quoteBatchCaller) { r.failure = status.Error(codes.Unavailable, "catalog offline") }, codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, rpc := quoteBatchSetup(t)
			tc.prepare(rpc)
			if _, err := m.Handle(context.Background(), batchQuoteCommand()); status.Code(err) != tc.code {
				t.Fatalf("wrong batch validation error: %v", err)
			}
			if rpc.batches != 1 || rpc.individual != 0 {
				t.Fatalf("failed batch retried individual reads: %#v", rpc)
			}
		})
	}
	m, rpc := quoteBatchSetup(t)
	c := batchQuoteCommand()
	c.Data["items"] = []platform.Result{{"sku_id": "sku-a", "quantity": 11}}
	if _, err := m.Handle(context.Background(), c); status.Code(err) != codes.InvalidArgument || rpc.batches != 0 {
		t.Fatalf("invalid quantity performed catalog reads: %v batches=%d", err, rpc.batches)
	}
}
