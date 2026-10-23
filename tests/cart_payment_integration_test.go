package tests

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"testing"
)

func TestPaidCartConsumesOnceAndPreservesNewQuantities(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	call := func(service, action, id, key string, data map[string]any) platform.Result {
		return c.call(t, ctx, service, action, id, key, "user", "demo", data)
	}
	call("cart", "add", "", "", map[string]any{"sku_id": "sku-watch", "quantity": 2})
	q := call("pricing", "quote", "", "", map[string]any{"items": []any{map[string]any{"sku_id": "sku-watch", "quantity": 1}}})
	placed := call("checkout", "submit", "", "cart-consume-once", map[string]any{"quote_id": platform.Text(q, "quote_id"), "source": "cart", "note": "Keep this delivery note", "address": map[string]any{"recipient": "Demo", "phone": "13800000000", "address": "Demo Street"}})
	oid := platform.Text(placed, "order_id")
	// Another quantity added after checkout must remain after the purchased unit is consumed.
	call("cart", "add", "", "", map[string]any{"sku_id": "sku-watch", "quantity": 1})
	p := call("payment", "create", oid, "", nil)
	c.call(t, ctx, "payment", "confirm_demo", platform.Text(p, "id"), "", "admin", "admin", nil)
	c.pump(t)
	o := call("order", "get", oid, "", nil)
	if platform.Text(o, "source") != "cart" || platform.Text(o, "note") != "Keep this delivery note" {
		t.Fatal(o)
	}
	assertCart := func() {
		rows := mapItems(call("cart", "list", "", "", nil))
		if len(rows) != 1 || platform.Int(rows[0], "quantity") != 2 {
			t.Fatalf("purchased quantity not consumed exactlyonce: %#v", rows)
		}
	}
	assertCart()
	var rows []platform.Outbox
	if err := c.dbs["order"].Where("event_type = ?", "OrderPaymentAccepted").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatal(len(rows))
	}
	var event platform.Event
	if err := json.Unmarshal([]byte(rows[0].PayloadJSON), &event); err != nil {
		t.Fatal(err)
	}
	// Even if an inbox delivery receipt is lost, the business receipt protects quantities.
	if err := c.modules["cart"].OnEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	assertCart()
}
