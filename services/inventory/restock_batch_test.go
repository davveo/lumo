package inventory_test

import (
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestRestockBatchIsAtomicAndIdempotent(t *testing.T) {
	db, m := setup(t, nil)
	before := must(t, m, "get", "sku-phone", "", "", nil)
	items := []map[string]any{
		{"id": "receipt-a", "sku_id": "sku-phone", "quantity": 2, "condition": "sellable"},
		{"id": "receipt-z", "sku_id": "zz-missing-stock", "quantity": 3, "condition": "sellable"},
	}
	data := map[string]any{"items": items}
	if _, e := invoke(m, "restock_batch", "", "demo", "system", data); status.Code(e) != codes.NotFound {
		t.Fatal(e)
	}
	var n int64
	if e := db.Table("ledgers").Where("id = ?", "receipt-a").Count(&n).Error; e != nil || n != 0 {
		t.Fatalf("partial ledger: %d %v", n, e)
	}
	after := must(t, m, "get", "sku-phone", "", "", nil)
	if platform.Int(after, "on_hand") != platform.Int(before, "on_hand") {
		t.Fatal("partial stock commit", after)
	}
	items[1]["sku_id"] = "sku-phone"
	items[1]["condition"] = "damaged"
	must(t, m, "restock_batch", "", "demo", "system", data)
	must(t, m, "restock_batch", "", "demo", "system", data)
	after = must(t, m, "get", "sku-phone", "", "", nil)
	if platform.Int(after, "on_hand") != platform.Int(before, "on_hand")+2 {
		t.Fatal("replay or damaged item increased stock", after)
	}
	items[0]["quantity"] = 9
	if _, e := invoke(m, "restock_batch", "", "demo", "system", data); status.Code(e) != codes.AlreadyExists {
		t.Fatal("receipt payload mismatch accepted", e)
	}
	if _, e := invoke(m, "restock_batch", "", "demo", "user", data); status.Code(e) != codes.PermissionDenied {
		t.Fatal("buyer mutated stock", e)
	}
}
