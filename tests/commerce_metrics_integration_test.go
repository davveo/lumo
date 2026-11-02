package tests

import (
	"context"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCommerceMetricsDeduplicatePaymentsAndRefunds(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	m := c.modules["analytics"]
	for _, kind := range []string{"view", "add_cart", "checkout"} {
		cmd := platform.Command{
			Action:         "record",
			UserID:         "demo",
			Role:           "user",
			IdempotencyKey: kind,
			Data:           map[string]any{"type": kind},
		}
		for i := 0; i < 2; i++ {
			if _, e := m.Handle(ctx, cmd); e != nil {
				t.Fatal(e)
			}
		}
	}
	events := []platform.Event{
		{
			ID:          "paid-first",
			Type:        "PaymentSucceeded",
			UserID:      "demo",
			AggregateID: "ORD_A",
			Data: map[string]any{
				"order_id":     "ORD_A",
				"amount_cents": 10000,
				"items":        []map[string]any{{"sku_id": "sku-coffee", "name": "Coffee", "quantity": 2}},
			},
		},
		{
			ID:          "paid-second",
			Type:        "PaymentSucceeded",
			UserID:      "demo",
			AggregateID: "ORD_B",
			Data:        map[string]any{"order_id": "ORD_B", "amount_cents": 20000},
		},
		{
			ID:          "refund-first",
			Type:        "RefundSucceeded",
			UserID:      "demo",
			AggregateID: "RFND_A",
			Data:        map[string]any{"order_id": "ORD_A", "amount_cents": 3000},
		},
		{
			ID:          "old-paid",
			Type:        "PaymentSucceeded",
			UserID:      "demo",
			AggregateID: "ORD_OLD",
			Data: map[string]any{
				"order_id":     "ORD_OLD",
				"amount_cents": 99999,
				"occurred_at":  time.Now().AddDate(0, 0, -100).UTC().Format(time.RFC3339),
			},
		},
	}
	for _, event := range events {
		for i := 0; i < 2; i++ {
			if e := m.OnEvent(ctx, event); e != nil {
				t.Fatal(e)
			}
		}
	}
	cmd := platform.Command{Action: "dashboard", UserID: "admin", Role: "admin", Data: map[string]any{"days": 7}}
	result, e := m.Handle(ctx, cmd)
	if e != nil {
		t.Fatal(e)
	}
	for key, want := range map[string]int64{"paid_cents": 30000, "refund_cents": 3000, "net_cents": 27000, "paid_orders": 2, "average_order_cents": 15000, "repeat_purchase_bps": 10000} {
		if platform.Int(result, key) != want {
			t.Fatal(key, result)
		}
	}
	for _, row := range result["funnel"].([]platform.Result) {
		if platform.Int(row, "cohort_users") != 1 || platform.Int(row, "users") != 1 {
			t.Fatal(result)
		}
	}
	cmd.Role = "user"
	if _, e = m.Handle(ctx, cmd); status.Code(e) != codes.PermissionDenied {
		t.Fatal("metrics leaked", e)
	}
}

func TestManagementDashboardSupportsNinetyDaysAcrossDomains(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	token := platform.Text(
		h.call("", "iam", "login", "", "", map[string]any{"username": "admin", "password": "Admin@2026"}),
		"token",
	)
	result := h.call(token, "management", "dashboard", "", "", map[string]any{"days": 90})
	if result["degraded"] != false || platform.Int(result, "days") != 90 {
		t.Fatal(result)
	}
	for _, key := range []string{"order", "payment", "refund", "fulfillment", "aftersale", "invoice", "users", "insights"} {
		part, ok := result[key].(map[string]any)
		if !ok || platform.Int(part, "days") != 90 {
			t.Fatalf("%s does not honor 90-day window: %v", key, result)
		}
	}
}

func TestMetricsHistoryBackfillIsIdempotent(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	at := time.Now().Add(-time.Hour).UTC()
	if err := c.dbs["payment"].Table("payments").Create(map[string]any{"id": "history-payment", "order_id": "history-order", "user_id": "demo", "amount": 9000, "status": "SUCCEEDED", "provider": "demo", "paid_at": at, "created_at": at, "updated_at": at}).Error; err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		for _, phase := range []string{"payment", "refund"} {
			if _, err := c.rpc.Call(ctx, "analytics", platform.Command{Action: "sync_history", UserID: "admin", Role: "admin", Data: map[string]any{"phase": phase}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	result, err := c.rpc.Call(
		ctx,
		"analytics",
		platform.Command{Action: "dashboard", UserID: "admin", Role: "admin", Data: map[string]any{"days": 7}},
	)
	if err != nil || platform.Int(result, "paid_cents") != 9000 || result["historical_backfill"] != true {
		t.Fatal(result, err)
	}
}
