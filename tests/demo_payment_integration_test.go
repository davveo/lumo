package tests

import (
	"context"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMemberDemoPaymentCompletesCheckoutExactlyOnce(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	call := func(service, action, id string, data map[string]any) platform.Result {
		return c.call(t, ctx, service, action, id, "", "user", "demo", data)
	}
	stockBefore := call("inventory", "get", "sku-watch", nil)
	pointsBefore := call("points", "get", "", nil)
	call("cart", "add", "", map[string]any{"sku_id": "sku-watch", "quantity": 3})
	quote := call("pricing", "quote", "", map[string]any{
		"items":     []any{map[string]any{"sku_id": "sku-watch", "quantity": 2}},
		"coupon_id": "coupon-demo-100",
		"points":    500,
	})
	placed := c.call(t, ctx, "checkout", "submit", "", "member-demo-payment", "user", "demo", map[string]any{
		"quote_id": platform.Text(quote, "quote_id"),
		"source":   "cart",
		"address": map[string]any{
			"recipient": "Demo Buyer",
			"phone":     "13800000000",
			"address":   "Integration Demo Street",
		},
	})
	orderID := platform.Text(placed, "order_id")
	if orderID == "" {
		t.Fatalf("checkout did not create an order: %v", placed)
	}
	payment := call("payment", "create", orderID, nil)
	paymentID := platform.Text(payment, "id")
	if paymentID == "" || platform.Text(payment, "status") != "PENDING" {
		t.Fatalf("create must return a pending payment request: %v", payment)
	}
	if order := call("order", "get", orderID, nil); platform.Text(order, "status") != "PENDING_PAYMENT" {
		t.Fatalf("creating a payment request must not mark the order paid: %v", order)
	}
	if _, err := c.rpc.Call(
		ctx,
		"fulfillment",
		platform.Command{Action: "get", ID: orderID, UserID: "demo", Role: "user"},
	); status.Code(
		err,
	) != codes.NotFound {
		t.Fatalf("unpaid order unexpectedly has warehouse work: %v", err)
	}

	// The buyer completes the explicit demo channel without an administrator.
	submitted := call("payment", "submit_demo", paymentID, nil)
	if platform.Text(submitted, "status") != "SUCCEEDED" || platform.Text(submitted, "receipt") == "" ||
		platform.Text(submitted, "paid_at") == "" ||
		platform.Int(submitted, "amount_cents") != platform.Int(quote, "payable_cents") {
		t.Fatalf("demo channel did not persist its successful receipt: %v", submitted)
	}
	assertReceiptUnchanged := func(result platform.Result) {
		t.Helper()
		for _, field := range []string{"id", "receipt", "paid_at", "status"} {
			if platform.Text(result, field) != platform.Text(submitted, field) {
				t.Fatalf("retry changed payment %s: original=%v retry=%v", field, submitted, result)
			}
		}
	}
	assertReceiptUnchanged(call("payment", "submit_demo", paymentID, nil))
	c.pump(t)

	assertCompleted := func() {
		t.Helper()
		order := call("order", "get", orderID, nil)
		committed, _ := order["resources_committed"].(bool)
		if platform.Text(order, "status") != "READY_TO_FULFILL" || platform.Text(order, "payment_id") != paymentID ||
			!committed {
			t.Fatalf("payment acceptance did not finish the resource saga: %v", order)
		}
		warehouse := call("fulfillment", "get", orderID, nil)
		if platform.Text(warehouse, "status") != "READY" || platform.Text(warehouse, "order_id") != orderID ||
			platform.Text(warehouse, "reservation_id") != orderID {
			t.Fatalf("paid order did not create matching warehouse work: %v", warehouse)
		}
		stock := call("inventory", "get", "sku-watch", nil)
		if platform.Int(stock, "on_hand") != platform.Int(stockBefore, "on_hand") ||
			platform.Int(stock, "reserved") != platform.Int(stockBefore, "reserved") ||
			platform.Int(stock, "allocated") != platform.Int(stockBefore, "allocated")+2 {
			t.Fatalf(
				"paid goods must be allocated once, without premature dispatch: before=%v after=%v",
				stockBefore,
				stock,
			)
		}
		points := call("points", "get", "", nil)
		if platform.Int(points, "available") != platform.Int(pointsBefore, "available")-500 ||
			platform.Int(points, "frozen") != 0 {
			t.Fatalf("payment did not consume the reserved points exactly once: %v", points)
		}
		couponUsed := false
		for _, coupon := range mapItems(call("coupon", "list", "", nil)) {
			if platform.Text(coupon, "id") == "coupon-demo-100" {
				couponUsed = platform.Text(coupon, "status") == "USED"
			}
		}
		if !couponUsed {
			t.Fatal("payment did not consume the reserved coupon")
		}
		cart := mapItems(call("cart", "list", "", nil))
		if len(cart) != 1 || platform.Text(cart[0], "sku_id") != "sku-watch" || platform.Int(cart[0], "quantity") != 1 {
			t.Fatalf("paid cart quantities must be consumed exactly once: %v", cart)
		}
		paidNotifications := 0
		for _, notification := range mapItems(call("notification", "list", "", nil)) {
			if platform.Text(notification, "event_type") == "OrderPaymentAccepted" &&
				platform.Text(notification, "business_id") == orderID {
				paidNotifications++
			}
		}
		if paidNotifications != 1 {
			t.Fatalf("expected one payment-accepted notification, got %d", paidNotifications)
		}
		for service, eventType := range map[string]string{
			"payment": "PaymentSucceeded",
			"order":   "OrderPaymentAccepted",
		} {
			var count int64
			if err := c.dbs[service].Model(&platform.Outbox{}).Where("event_type = ?", eventType).Count(
				&count,
			).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("%s emitted %s %d times", service, eventType, count)
			}
		}
	}
	assertCompleted()
	// A lost HTTP response can be retried after the order is already paid.
	assertReceiptUnchanged(call("payment", "submit_demo", paymentID, nil))
	c.pump(t)
	assertCompleted()
}
