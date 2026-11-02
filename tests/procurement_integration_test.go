package tests

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestProcurementPartialReceiptsAreAtomicAndIdempotent(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	role := "admin"
	call := func(action, id, key string, data map[string]any) (platform.Result, error) {
		return c.rpc.Call(
			ctx,
			"inventory",
			platform.Command{Action: action, ID: id, UserID: "admin", Role: role, IdempotencyKey: key, Data: data},
		)
	}
	initial, e := call("get", "sku-coffee", "", nil)
	if e != nil {
		t.Fatal(e)
	}
	job, e := call(
		"procurement_create",
		"",
		"purchase-request-1",
		map[string]any{
			"supplier": "Test Supplier",
			"lines":    []map[string]any{{"sku_id": "sku-coffee", "quantity": 10, "unit_cost_cents": 1000}},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	id := platform.Text(job, "id")
	if _, e = call("procurement_approve", id, "", map[string]any{"expected_version": 0}); e != nil {
		t.Fatal(e)
	}
	receipt := map[string]any{
		"expected_version": 1,
		"lines":            []map[string]any{{"sku_id": "sku-coffee", "quantity": 4, "damaged": 1}},
	}
	for i := 0; i < 2; i++ {
		job, e = call("procurement_receive", id, "receipt-request-1", receipt)
		if e != nil {
			t.Fatal(e)
		}
	}
	if job["status"] != "PARTIALLY_RECEIVED" {
		t.Fatal(job)
	}
	stock, e := call("get", "sku-coffee", "", nil)
	if e != nil || platform.Int(stock, "on_hand") != platform.Int(initial, "on_hand")+3 {
		t.Fatal(stock, e)
	}
	if _, e = call("procurement_receive", id, "receipt-too-much", map[string]any{"expected_version": 2, "lines": []map[string]any{{"sku_id": "sku-coffee", "quantity": 7}}}); status.Code(
		e,
	) != codes.FailedPrecondition {
		t.Fatal(e)
	}
	job, e = call(
		"procurement_receive",
		id,
		"receipt-request-2",
		map[string]any{"expected_version": 2, "lines": []map[string]any{{"sku_id": "sku-coffee", "quantity": 6}}},
	)
	if e != nil || job["status"] != "RECEIVED" {
		t.Fatal(job, e)
	}
	stock, e = call("get", "sku-coffee", "", nil)
	if e != nil || platform.Int(stock, "on_hand") != platform.Int(initial, "on_hand")+9 {
		t.Fatal(stock, e)
	}
	role = "user"
	if _, e = call("procurement_list", "", "", nil); status.Code(e) != codes.PermissionDenied {
		t.Fatal(e)
	}
}
