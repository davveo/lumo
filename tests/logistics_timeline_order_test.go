package tests

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	"testing"
	"time"
)

func TestTrackingPagesByOccurrenceInsteadOfCallbackArrival(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	waybill := c.call(
		t,
		ctx,
		"logistics",
		"create",
		"timeline-order-shipment",
		"",
		"system",
		"demo",
		map[string]any{"order_id": "timeline-order"},
	)
	now := time.Now().UTC()
	rows := []map[string]any{}
	for i := 0; i < 101; i++ {
		rows = append(
			rows,
			map[string]any{
				"id":           fmt.Sprintf("chronology-%03d", i),
				"logistics_id": "timeline-order-shipment",
				"tracking_id":  waybill["tracking_id"],
				"status":       "IN_TRANSIT",
				"occurred_at":  now.Add(-time.Duration(i) * time.Minute),
				"created_at":   now.Add(time.Duration(i) * time.Millisecond),
			},
		)
	}
	if err := c.dbs["logistics"].Table("tracking_events").Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	value := c.call(t, ctx, "logistics", "tracking", "timeline-order-shipment", "", "user", "demo", nil)
	var detail struct {
		Shipments []struct {
			Events []struct {
				ID string `json:"id"`
			} `json:"events"`
			HasMore bool `json:"has_more_events"`
		} `json:"shipments"`
	}
	if err := platform.Decode(value, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Shipments) != 1 || len(detail.Shipments[0].Events) != 100 ||
		detail.Shipments[0].Events[0].ID != "chronology-000" ||
		!detail.Shipments[0].HasMore {
		t.Fatal("latest scan is hidden by delayed old callbacks", detail)
	}
	for i, state := range []string{"IN_TRANSIT", "CREATED"} {
		c.call(
			t,
			ctx,
			"logistics",
			"tracking_update",
			fmt.Sprintf("timeline-state-%d", i),
			"",
			"admin",
			"admin",
			map[string]any{
				"tracking_id": waybill["tracking_id"],
				"status":      state,
				"occurred_at": now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			},
		)
	}
	current := c.call(t, ctx, "logistics", "get", "timeline-order-shipment", "", "user", "demo", nil)
	if platform.Text(current, "status") != "IN_TRANSIT" {
		t.Fatal("created scan regressed a moving shipment", current)
	}
}
