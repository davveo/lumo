package tests

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"testing"
	"time"
)

func TestPhysicalPointsRewardUsesRealStockAndSeparateShipmentEvents(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
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
			"address":   "测试路1号",
		},
	)
	before := c.call(t, ctx, "inventory", "get", "sku-coffee", "", "user", "demo", nil)
	offer := c.call(
		t,
		ctx,
		"points",
		"offer_upsert",
		"",
		"",
		"admin",
		"admin",
		map[string]any{
			"name":         "咖啡兑换",
			"kind":         "GIFT",
			"reference_id": "sku-coffee",
			"cost":         200,
			"total_quota":  2,
			"max_per_user": 1,
			"active":       true,
			"starts_at":    time.Now().Add(-time.Hour).Format(time.RFC3339),
			"ends_at":      time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	)
	claim := c.call(
		t,
		ctx,
		"points",
		"redeem",
		platform.Text(offer, "id"),
		"gift-request-unique",
		"user",
		"demo",
		map[string]any{"version": platform.Int(offer, "version"), "address_id": platform.Text(address, "id")},
	)
	c.pump(t)
	id := platform.Text(claim, "id")
	row := c.call(t, ctx, "points", "redemption_get", id, "", "user", "demo", nil)
	if platform.Text(row, "state") != "ISSUED" {
		t.Fatal(row)
	}
	job := c.call(t, ctx, "fulfillment", "get", id, "", "admin", "admin", nil)
	if platform.Text(job, "kind") != "GIFT" || platform.Text(job, "address_json") == "" {
		t.Fatal(job)
	}
	c.call(t, ctx, "fulfillment", "dispatch", id, "", "admin", "admin", nil)
	c.pump(t)
	row = c.call(t, ctx, "points", "redemption_get", id, "", "user", "demo", nil)
	if platform.Text(row, "state") != "SHIPPED" {
		t.Fatal(row)
	}
	c.call(t, ctx, "logistics", "deliver", id, "", "admin", "admin", nil)
	c.pump(t)
	row = c.call(t, ctx, "points", "redemption_get", id, "", "user", "demo", nil)
	if platform.Text(row, "state") != "DELIVERED" {
		t.Fatal(row)
	}
	after := c.call(t, ctx, "inventory", "get", "sku-coffee", "", "user", "demo", nil)
	if platform.Int(after, "on_hand") != platform.Int(before, "on_hand")-1 || platform.Int(after, "reserved") != 0 ||
		platform.Int(after, "allocated") != 0 {
		t.Fatal(after)
	}
	balance := c.call(t, ctx, "points", "get", "", "", "user", "demo", nil)
	if platform.Int(balance, "available") != 4800 {
		t.Fatal(balance)
	}
	orders := c.call(t, ctx, "order", "list", "", "", "user", "demo", nil)
	var list struct{ Items []platform.Result }
	_ = platform.Decode(orders, &list)
	if len(list.Items) != 0 {
		t.Fatal("reward created financial order", orders)
	}
	c.call(t, ctx, "analytics", "drilldown", "", "", "admin", "admin", map[string]any{"days": 7, "dimension": "source"})
}
