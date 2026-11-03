package fulfillment

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestRecoveryScanOnlyQueuesWorkAndConsumerRetriesOneShipment(t *testing.T) {
	calls := 0
	fail := true
	rpc := &fakeCaller{fn: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
		calls++
		if fail {
			return nil, status.Error(codes.Unavailable, "warehouse unavailable")
		}
		if service == "logistics" {
			return platform.Result{"tracking_id": "track"}, nil
		}
		return platform.Result{}, nil
	}}
	m, db := setup(t, rpc)
	ctx := context.Background()
	if e := m.OnEvent(ctx, platform.Event{
		ID:          "ready",
		Type:        "OrderReadyToFulfill",
		AggregateID: "order-recovery",
		UserID:      "buyer",
		Data:        map[string]any{"order_id": "order-recovery"},
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Handle(ctx, platform.Command{Action: "dispatch", ID: "order-recovery", UserID: "admin", Role: "admin"}); status.Code(
		e,
	) != codes.Unavailable {
		t.Fatal(e)
	}
	calls = 0
	if e := m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if calls != 0 {
		t.Fatal("recovery scan called remote service", calls)
	}
	var rows []platform.Outbox
	if e := db.Where("event_type = ?", "FulfillmentRecoveryRequested").Find(&rows).Error; e != nil || len(rows) != 1 {
		t.Fatalf("recovery not durable: %d %v", len(rows), e)
	}
	var work platform.Event
	if e := json.Unmarshal([]byte(rows[0].PayloadJSON), &work); e != nil {
		t.Fatal(e)
	}
	fail = false
	if e := m.OnEvent(ctx, work); e != nil {
		t.Fatal(e)
	}
	if calls != 3 {
		t.Fatalf("expected order dispatch gate + inventory + logistics for one item: %d", calls)
	}
	calls = 0
	if e := m.OnEvent(ctx, work); e != nil || calls != 0 {
		t.Fatalf("stale work retried shipment: calls=%d %v", calls, e)
	}
}

func TestInspectReturnsUsesOneStockRPCAndReplaysWithoutRestocking(t *testing.T) {
	calls := 0
	fail := true
	rpc := &fakeCaller{fn: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
		calls++
		if service != "inventory" || c.Action != "restock_batch" || c.Role != "system" || c.UserID != "demo" {
			t.Fatalf("unexpected RPC: %s %#v", service, c)
		}
		var in struct {
			Items []platform.Result `json:"items"`
		}
		if e := platform.Decode(c.Data, &in); e != nil || len(in.Items) != 2 {
			t.Fatalf("lost batch: %v %v", in, e)
		}
		if fail {
			return nil, status.Error(codes.Unavailable, "retry")
		}
		return platform.Result{}, nil
	}}
	m, db := setup(t, rpc)
	cmd := platform.Command{
		Action: "inspect_returns",
		UserID: "demo",
		Role:   "system",
		Data: map[string]any{"items": []map[string]any{
			{"id": "r1", "order_id": "o1", "sku_id": "sku-a", "quantity": 1, "condition": "sellable"},
			{"id": "r2", "order_id": "o1", "sku_id": "sku-b", "quantity": 2, "condition": "damaged"},
		}},
	}
	if _, e := m.Handle(context.Background(), cmd); status.Code(e) != codes.Unavailable {
		t.Fatal(e)
	}
	var count int64
	if e := db.Table("records").Where("status = ?", "INSPECTED").Count(&count).Error; e != nil || count != 0 {
		t.Fatalf("failed RPC marked receipts complete: %d %v", count, e)
	}
	fail = false
	calls = 0
	run(t, m, cmd)
	run(t, m, cmd)
	if calls != 1 {
		t.Fatalf("multi-item inspection/replay RPCs = %d", calls)
	}
	cmd.UserID = "other"
	if _, e := m.Handle(context.Background(), cmd); status.Code(e) != codes.PermissionDenied {
		t.Fatal("receipt ownership lost", e)
	}
}
