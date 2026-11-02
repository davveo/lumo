package tests

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"testing"
)

func TestCatalogUpsertCannotReassignAnotherProductsSKU(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	upsert := func(id string, variants bool) (platform.Result, error) {
		data := map[string]any{"name": id, "price_cents": 1200, "active": true}
		if variants {
			data["colors"] = []string{"红色", "蓝色"}
			data["options"] = []string{"标准"}
		}
		return c.rpc.Call(
			ctx,
			"catalog",
			platform.Command{Action: "upsert", ID: id, UserID: "admin", Role: "admin", Data: data},
		)
	}
	if _, err := upsert("sku-owner", true); err != nil {
		t.Fatal(err)
	}
	if _, err := upsert("sku-owner--1-0", false); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("child SKU replaced by product: %v", err)
	}
	child, err := c.rpc.Call(ctx, "catalog", platform.Command{Action: "get", ID: "sku-owner--1-0"})
	if err != nil || platform.Text(child, "product_id") != "sku-owner" {
		t.Fatal(child, err)
	}
	if _, err = upsert("sku-conflict--1-0", false); err != nil {
		t.Fatal(err)
	}
	if _, err = upsert("sku-conflict", true); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("product replaced by child SKU: %v", err)
	}
	if _, err = c.rpc.Call(ctx, "catalog", platform.Command{Action: "get", ID: "sku-conflict"}); status.Code(
		err,
	) != codes.NotFound {
		t.Fatal("conflicting upsert was not atomic", err)
	}
	if _, err = upsert("sku-owner", true); err != nil {
		t.Fatal("normal update rejected", err)
	}
	if _, err = upsert(strings.Repeat("x", 126), true); status.Code(err) != codes.InvalidArgument {
		t.Fatal("oversized derived SKU accepted", err)
	}
}
