package devstack

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"testing"
)

func TestRestartDeliversDurablePaymentWithoutDoubleAllocation(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	ctx := context.Background()
	dir := t.TempDir()
	stack, err := Start(ctx, dir, "first-internal-test-credential")
	if err != nil {
		t.Fatal(err)
	}
	call := func(service, action, id, key, role, user string, data map[string]any) platform.Result {
		t.Helper()
		out, e := stack.RPC.Call(ctx, service, platform.Command{Action: action, ID: id, IdempotencyKey: key, Role: role, UserID: user, Data: data})
		if e != nil {
			t.Fatalf("%s.%s %v", service, action, e)
		}
		return out
	}
	q := call("pricing", "quote", "", "", "user", "demo", map[string]any{"items": []any{map[string]any{"sku_id": "sku-watch", "quantity": 1}}})
	order := call("checkout", "submit", "", "restart-order", "user", "demo", map[string]any{"quote_id": platform.Text(q, "quote_id"), "address": map[string]any{"recipient": "Demo", "phone": "13800000000", "address": "Integration street"}})
	oid := platform.Text(order, "order_id")
	p := call("payment", "create", oid, "", "user", "demo", nil)
	call("payment", "confirm_demo", platform.Text(p, "id"), "", "admin", "admin", nil)
	// Simulate process exit after the money transaction commits but before broker delivery.
	stack.Close()
	stack, err = Start(ctx, dir, "next-internal-test-credential")
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()
	if err = stack.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	ready := call("order", "get", oid, "", "user", "demo", nil)
	if platform.Text(ready, "status") != "READY_TO_FULFILL" {
		t.Fatal(ready)
	}
	assertStock := func() {
		s := call("inventory", "get", "sku-watch", "", "user", "demo", nil)
		if platform.Int(s, "on_hand") != 50 || platform.Int(s, "allocated") != 1 || platform.Int(s, "available") != 49 {
			t.Fatal(s)
		}
	}
	assertStock()
	if err = stack.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	assertStock()
}
