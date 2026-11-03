package tests

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestOrderSelfServiceUsesOwnedAddressesAndCreatesDurableUrgeTicket(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	id := c.place(t, ctx, "sku-coffee", "demo", "selfservice", "")
	c.pay(t, ctx, id, "demo")
	address := c.call(
		t,
		ctx,
		"address",
		"create",
		"",
		"",
		"user",
		"demo",
		map[string]any{
			"recipient": "体验用户",
			"phone":     "13800000000",
			"province":  "上海市",
			"city":      "上海市",
			"district":  "浦东新区",
			"address":   "示例路 2 号",
		},
	)
	o := c.call(t, ctx, "order", "get", id, "", "user", "demo", nil)
	changed := c.call(
		t,
		ctx,
		"checkout",
		"change_address",
		id,
		"",
		"user",
		"demo",
		map[string]any{"address_id": platform.Text(address, "id"), "expected_version": platform.Int(o, "version")},
	)
	if platform.Int(changed, "version") <= platform.Int(o, "version") {
		t.Fatal(changed)
	}
	c.call(t, ctx, "order", "urge", id, "", "user", "demo", nil)
	c.pump(t)
	tickets := c.call(t, ctx, "helpdesk", "list", "", "", "user", "demo", map[string]any{"order_id": id})
	var page struct{ Items []platform.Result }
	_ = platform.Decode(tickets, &page)
	if len(page.Items) != 1 || platform.Text(page.Items[0], "order_id") != id {
		t.Fatal(tickets)
	}
	timeline := c.call(t, ctx, "checkout", "timeline", id, "", "user", "demo", nil)
	var view struct{ Events []platform.Result }
	_ = platform.Decode(timeline, &view)
	found := false
	for _, event := range view.Events {
		if platform.Text(event, "status") == "shipment_urged" {
			found = true
		}
	}
	if !found {
		t.Fatal(timeline)
	}
	_, e := c.rpc.Call(ctx, "checkout", platform.Command{Action: "timeline", ID: id, UserID: "other", Role: "user"})
	if status.Code(e) != codes.PermissionDenied {
		t.Fatal("timeline leaked", e)
	}
	c.call(t, ctx, "fulfillment", "dispatch", id, "", "admin", "admin", nil)
	c.pump(t)
	_, e = c.rpc.Call(
		ctx,
		"checkout",
		platform.Command{
			Action: "change_address",
			ID:     id,
			UserID: "demo",
			Role:   "user",
			Data: map[string]any{
				"address_id":       platform.Text(address, "id"),
				"expected_version": platform.Int(changed, "version"),
			},
		},
	)
	if e == nil {
		t.Fatal("address mutation accepted after dispatch")
	}
}
