package pricing_test

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	pricingmod "github.com/davveo/lumo/services/pricing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"testing"
)

type catalog struct {
	price         int64
	lastThreshold int64
}

func (c *catalog) Call(ctx context.Context, name string, in platform.Command) (platform.Result, error) {
	switch name {
	case "catalog":
		row := func(id string) platform.Result {
			price := c.price
			if id == "sku-coffee" {
				price = 39900
			}
			return platform.Result{"id": id, "name": "Demo SKU", "price_cents": price, "active": true}
		}
		if in.Action == "batch_get" {
			var data struct {
				IDs []string `json:"ids"`
			}
			if err := platform.Decode(in.Data, &data); err != nil {
				return nil, err
			}
			items := []platform.Result{}
			for _, id := range data.IDs {
				items = append(items, row(id))
			}
			return platform.Result{"items": items, "missing_ids": []string{}}, nil
		}
		return row(in.ID), nil
	case "campaign":
		c.lastThreshold = platform.Int(in.Data, "subtotal_cents")
		eligible := platform.Text(in.Data, "addon_sku_id") == "" || c.lastThreshold >= 100000
		return platform.Result{"discount_cents": int64(0), "eligible": eligible, "policy_version": "v1"}, nil
	case "points":
		return platform.Result{"available": int64(5000)}, nil
	}
	return nil, status.Error(codes.Unimplemented, "unexpected pricing dependency")
}
func setup(t *testing.T) (platform.Module, *catalog) {
	t.Helper()
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "pricing.db"))
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { _ = sql.Close() })
	p := &catalog{price: 1000}
	m := pricingmod.New(platform.Dependencies{DB: db, RPC: p})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	return m, p
}
func TestQuoteIgnoresFrontendPricesAndStaysImmutable(t *testing.T) {
	m, p := setup(t)
	ctx := context.Background()
	v, e := m.Handle(
		ctx,
		platform.Command{
			Action: "quote",
			UserID: "demo",
			Role:   "user",
			Data: map[string]any{
				"items": []any{map[string]any{"sku_id": "sku-test", "quantity": 1, "unit_price_cents": 1}},
			},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	if platform.Int(v, "payable_cents") != 1000 {
		t.Fatalf("trusted frontend price: %v", v)
	}
	p.price = 2000
	old, e := m.Handle(
		ctx,
		platform.Command{Action: "get", ID: platform.Text(v, "quote_id"), UserID: "demo", Role: "user"},
	)
	if e != nil || platform.Int(old, "payable_cents") != 1000 {
		t.Fatalf("quote snapshot changed with catalog: %v %v", old, e)
	}
	_, e = m.Handle(
		ctx,
		platform.Command{Action: "get", ID: platform.Text(v, "quote_id"), UserID: "other", Role: "user"},
	)
	if status.Code(e) != codes.PermissionDenied {
		t.Fatalf("quote ownership bypass: %v", e)
	}
}
func TestAddonDoesNotCountTowardItsOwnThreshold(t *testing.T) {
	m, p := setup(t)
	p.price = 70000
	_, e := m.Handle(
		context.Background(),
		platform.Command{
			Action: "quote",
			UserID: "demo",
			Role:   "user",
			Data: map[string]any{
				"items":        []any{map[string]any{"sku_id": "sku-test", "quantity": 1}},
				"addon_sku_id": "sku-coffee",
			},
		},
	)
	if status.Code(e) != codes.FailedPrecondition || p.lastThreshold != 70000 {
		t.Fatalf("add-on self-qualified threshold=%d error=%v", p.lastThreshold, e)
	}
}
func TestNegativeQuantityAndExcessPointsRejected(t *testing.T) {
	m, _ := setup(t)
	for _, data := range []map[string]any{
		{"items": []any{map[string]any{"sku_id": "sku-test", "quantity": -1}}},
		{"items": []any{map[string]any{"sku_id": "sku-test", "quantity": 1}}, "points": 500},
	} {
		if _, e := m.Handle(
			context.Background(),
			platform.Command{Action: "quote", UserID: "demo", Role: "user", Data: data},
		); e == nil {
			t.Fatalf("invalid quote accepted: %v", data)
		}
	}
}
