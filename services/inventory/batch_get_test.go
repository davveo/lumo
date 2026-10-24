package inventory_test

import (
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func TestBatchGetUsesOneSQLAndPreservesSKUAvailability(t *testing.T) {
	db, module := setup(t, nil)
	must(t, module, "reserve", "held-stock", "buyer", "system", map[string]any{"order_id": "held-stock", "lines": []map[string]any{{"sku_id": "sku-phone", "quantity": 2}}})
	queries := 0
	if err := db.Callback().Query().Before("gorm:query").Register("batch-get-query-count", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	out, err := invoke(module, "batch_get", "", "", "", map[string]any{"ids": []string{"sku-phone", "sku-watch", "sku-phone", "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	items := out["items"].([]platform.Result)
	missing := out["missing_ids"].([]string)
	if queries != 1 || len(items) != 2 || platform.Text(items[0], "sku_id") != "sku-phone" || platform.Int(items[0], "available") != 48 || len(missing) != 1 || missing[0] != "missing" {
		t.Fatalf("batch query count/order/availability wrong: queries=%d rows=%v", queries, out)
	}
	for _, ids := range [][]string{nil, {""}, {" sku-phone"}, make([]string, 501)} {
		_, err = invoke(module, "batch_get", "", "", "", map[string]any{"ids": ids})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid ids accepted: %v", ids)
		}
	}
}
