package tests

import (
	"context"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCampaignTypesCompleteCheckoutAndPreserveDiscounts(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	activity := func(t *testing.T, id string) platform.Result {
		t.Helper()
		for _, row := range mapItems(c.call(t, ctx, "campaign", "list", "", "", "user", "demo", nil)) {
			if platform.Text(row, "id") == id {
				return row
			}
		}
		t.Fatalf("activity %s was not published", id)
		return nil
	}
	address := map[string]any{"recipient": "Campaign Buyer", "phone": "13800000000", "address": "Integration Campaign Street"}
	for _, tc := range []struct {
		id, kind, sku, user string
		quantity, fixed     int64
		payRate             int64
	}{
		{id: "reduction-demo", kind: "reduction", sku: "sku-coffee", user: "campaign-reduction", quantity: 1, fixed: 3000},
		{id: "discount-demo", kind: "discount", sku: "sku-watch", user: "demo", quantity: 1, payRate: 9000},
		{id: "quantity-discount-demo", kind: "quantity_discount", sku: "sku-coffee", user: "campaign-quantity", quantity: 2, payRate: 8500},
		{id: "direct-demo", kind: "direct", sku: "sku-coffee", user: "campaign-direct", quantity: 1, fixed: 2000},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			call := func(service, action, id, key string, data map[string]any) platform.Result {
				return c.call(t, ctx, service, action, id, key, "user", tc.user, data)
			}
			before := activity(t, tc.id)
			if platform.Text(before, "kind") != tc.kind {
				t.Fatalf("published activity kind mismatch: %v", before)
			}
			product := call("catalog", "get", tc.sku, "", nil)
			subtotal := platform.Int(product, "price_cents") * tc.quantity
			wantDiscount := tc.fixed
			if tc.payRate > 0 {
				wantDiscount = subtotal * (10000 - tc.payRate) / 10000
			}
			data := map[string]any{"activity_id": tc.id, "items": []any{map[string]any{"sku_id": tc.sku, "quantity": tc.quantity, "unit_price_cents": 1}}}
			if tc.kind == "discount" {
				// Ordinary campaign discounts retain the existing coupon/points path.
				data["coupon_id"] = "coupon-demo-100"
				data["points"] = int64(500)
			}
			quote := call("pricing", "quote", "", "", data)
			if platform.Int(quote, "subtotal_cents") != subtotal || platform.Int(quote, "campaign_discount_cents") != wantDiscount || platform.Text(quote, "activity_kind") != tc.kind || platform.Text(quote, "activity_name") == "" {
				t.Fatalf("campaign quote lost authoritative price or activity snapshot: %v", quote)
			}
			wantPayable := subtotal - wantDiscount - platform.Int(quote, "coupon_discount_cents") - platform.Int(quote, "points")
			if platform.Int(quote, "payable_cents") != wantPayable {
				t.Fatalf("campaign, coupon and points were not composed correctly: %v", quote)
			}
			placed := call("checkout", "submit", "", "campaign-paid-"+tc.id, map[string]any{"quote_id": quote["quote_id"], "address": address})
			oid := platform.Text(placed, "order_id")
			order := call("order", "get", oid, "", nil)
			storedQuote := platform.Map(order["quote"])
			if platform.Text(order, "status") != "PENDING_PAYMENT" || platform.Int(order, "payable_cents") != wantPayable || platform.Int(storedQuote, "campaign_discount_cents") != wantDiscount || platform.Text(storedQuote, "activity_kind") != tc.kind {
				t.Fatalf("checkout lost the promotion amount or metadata: %v", order)
			}
			reserved := activity(t, tc.id)
			if platform.Int(reserved, "held") != platform.Int(before, "held")+1 || platform.Int(reserved, "used") != platform.Int(before, "used") {
				t.Fatalf("checkout did not reserve one campaign admission: before=%v after=%v", before, reserved)
			}
			payment := call("payment", "create", oid, "", nil)
			call("payment", "submit_demo", platform.Text(payment, "id"), "", nil)
			c.pump(t)
			order = call("order", "get", oid, "", nil)
			committed, _ := order["resources_committed"].(bool)
			if platform.Text(order, "status") != "READY_TO_FULFILL" || !committed || platform.Int(order, "payable_cents") != wantPayable {
				t.Fatalf("campaign order did not finish payment/resource confirmation: %v", order)
			}
			if warehouse := call("fulfillment", "get", oid, "", nil); platform.Text(warehouse, "status") != "READY" {
				t.Fatalf("campaign order did not create warehouse work: %v", warehouse)
			}
			confirmed := activity(t, tc.id)
			if platform.Int(confirmed, "held") != platform.Int(before, "held") || platform.Int(confirmed, "used") != platform.Int(before, "used")+1 {
				t.Fatalf("payment did not confirm campaign admission: before=%v after=%v", before, confirmed)
			}
			call("payment", "submit_demo", platform.Text(payment, "id"), "", nil)
			c.pump(t)
			if retried := activity(t, tc.id); platform.Int(retried, "held") != platform.Int(confirmed, "held") || platform.Int(retried, "used") != platform.Int(confirmed, "used") {
				t.Fatalf("payment retry consumed another campaign admission: %v", retried)
			}

			if tc.kind == "discount" {
				after := call("aftersale", "create", "", "campaign-discount-refund", map[string]any{"order_id": oid, "type": "refund", "reason": "Verify actual discounted cash refund"})
				if platform.Int(after, "amount_cents") != wantPayable {
					t.Fatalf("refund claim used original price instead of actual payment: %v", after)
				}
				asid := platform.Text(after, "id")
				c.call(t, ctx, "aftersale", "approve", asid, "", "admin", "admin", nil)
				refunding := c.call(t, ctx, "aftersale", "refund", asid, "", "admin", "admin", nil)
				c.call(t, ctx, "refund", "confirm_demo", platform.Text(refunding, "refund_id"), "", "admin", "admin", nil)
				c.pump(t)
				refunded := call("order", "get", oid, "", nil)
				if platform.Int(refunded, "refunded_total_cents") != wantPayable {
					t.Fatalf("discounted full refund did not settle the actual cash amount: %v", refunded)
				}
				if completed := call("aftersale", "get", asid, "", nil); platform.Text(completed, "status") != "COMPLETED" {
					t.Fatalf("discounted cash refund did not complete the aftersale: %v", completed)
				}
				if afterRefund := activity(t, tc.id); platform.Int(afterRefund, "used") != platform.Int(confirmed, "used") {
					t.Fatalf("refund unexpectedly restored a confirmed activity admission: %v", afterRefund)
				}
			}

			// Unpaid cancellation releases the reserved admission exactly once.
			cancelUser := "cancel-" + tc.kind
			cancelData := map[string]any{"activity_id": tc.id, "items": []any{map[string]any{"sku_id": tc.sku, "quantity": tc.quantity}}}
			cancelQuote := c.call(t, ctx, "pricing", "quote", "", "", "user", cancelUser, cancelData)
			cancelled := c.call(t, ctx, "checkout", "submit", "", "campaign-cancel-"+tc.id, "user", cancelUser, map[string]any{"quote_id": cancelQuote["quote_id"], "address": address})
			cancelID := platform.Text(cancelled, "order_id")
			if held := activity(t, tc.id); platform.Int(held, "held") != platform.Int(confirmed, "held")+1 {
				t.Fatalf("second member did not reserve campaign admission: %v", held)
			}
			c.call(t, ctx, "checkout", "cancel", cancelID, "", "user", cancelUser, nil)
			c.pump(t)
			c.call(t, ctx, "checkout", "cancel", cancelID, "", "user", cancelUser, nil)
			if released := activity(t, tc.id); platform.Int(released, "held") != platform.Int(confirmed, "held") || platform.Int(released, "used") != platform.Int(confirmed, "used") {
				t.Fatalf("cancel did not release the unconfirmed campaign admission exactly once: %v", released)
			}
			if order := c.call(t, ctx, "order", "get", cancelID, "", "user", cancelUser, nil); platform.Text(order, "status") != "CLOSED" {
				t.Fatalf("unpaid campaign cancellation left the order open: %v", order)
			}
		})
	}
}

func TestCampaignQuotesRemainImmutableAndRejectIncompatibleActivities(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	items := []any{map[string]any{"sku_id": "sku-coffee", "quantity": 2}}
	quote := c.call(t, ctx, "pricing", "quote", "", "", "user", "demo", map[string]any{"activity_id": "direct-demo", "items": items})
	if err := c.dbs["campaign"].Table("activities").Where("id = ?", "direct-demo").Update("discount", 2500).Error; err != nil {
		t.Fatal(err)
	}
	stored := c.call(t, ctx, "pricing", "get", platform.Text(quote, "quote_id"), "", "user", "demo", nil)
	for _, field := range []string{"subtotal_cents", "campaign_discount_cents", "payable_cents"} {
		if platform.Int(stored, field) != platform.Int(quote, field) {
			t.Fatalf("campaign policy change mutated quote %s: original=%v current=%v", field, quote, stored)
		}
	}
	_, err := c.rpc.Call(ctx, "checkout", platform.Command{Action: "submit", IdempotencyKey: "stale-campaign-policy", UserID: "demo", Role: "user", Data: map[string]any{"quote_id": quote["quote_id"], "address": map[string]any{"recipient": "Campaign", "phone": "13800000000", "address": "Integration Street"}}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("checkout accepted an outdated campaign policy: %v", err)
	}
	team := c.call(t, ctx, "group", "create", "", "campaign-conflict-team", "user", "demo", map[string]any{"sku_id": "sku-coffee", "required": 2})
	for _, data := range []map[string]any{
		{"activity_id": "reduction-demo", "group_id": team["id"], "items": []any{map[string]any{"sku_id": "sku-coffee", "quantity": 1}}},
		{"activity_id": "discount-demo", "addon_sku_id": "sku-coffee", "items": items},
	} {
		_, err := c.rpc.Call(ctx, "pricing", platform.Command{Action: "quote", UserID: "demo", Role: "user", Data: data})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("incompatible activity combination was accepted: %v, error=%v", data, err)
		}
	}
}
