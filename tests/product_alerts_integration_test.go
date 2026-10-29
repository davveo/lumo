package tests

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

func TestProductAlertsCustomerAndAdministration(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	login := func(name, pw string) string {
		return platform.Text(h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": pw}), "token")
	}
	admin, buyer := login("admin", "Admin@2026"), login("demo", "Lumo@2026")
	policy := h.call("", "engagement", "policy_get", "", "", nil)
	config := policy["config"].(map[string]any)
	h.expect(401, "", "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-coffee", "kind": "price", "target_cents": 30000})
	h.expect(403, buyer, "engagement", "admin_list", "", "", nil)
	h.expect(403, buyer, "engagement", "policy_upsert", "", "", map[string]any{"config": config, "reason": "forged"})
	h.expect(400, buyer, "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-coffee", "kind": "price", "target_cents": 39900})
	h.expect(409, buyer, "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-coffee", "kind": "stock"})
	price := h.call(buyer, "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-coffee", "kind": "price", "target_cents": 30000})
	id := platform.Text(price, "id")
	if !platform.IsBusinessID(id, "alert") {
		t.Fatal(price)
	}
	again := h.call(buyer, "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-coffee", "kind": "price", "target_cents": 30000})
	if again["id"] != id || again["expires_at"] != price["expires_at"] {
		t.Fatal("a retry changed subscription", again)
	}
	h.expect(409, buyer, "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-coffee", "kind": "price", "target_cents": 20000})
	h.call(admin, "catalog", "upsert", "sku-coffee", "", map[string]any{"name": "提醒测试咖啡机", "price_cents": 29000, "active": true})
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if e := c.modules["engagement"].Tick(ctx); e != nil {
				t.Error(e)
			}
		})
	}
	workers.Wait()
	if e := c.modules["engagement"].Tick(ctx); e != nil {
		t.Fatal(e)
	}
	c.pump(t)
	list := h.call(buyer, "engagement", "subscriptions", "", "", map[string]any{"limit": 1})
	if platform.Int(list, "total") != 1 || platform.Text(mapItems(list)[0], "status") != "TRIGGERED" {
		t.Fatal(list)
	}
	messages := mapItems(h.call(buyer, "notification", "list", "", "", map[string]any{"limit": 100}))
	count := 0
	for _, r := range messages {
		if platform.Text(r, "event_type") == "ProductPriceReached" {
			count++
			if platform.Text(r, "sku_id") != "sku-coffee" || !platform.IsBusinessID(platform.Text(r, "id"), "notification") {
				t.Fatal(r)
			}
		}
	}
	if count != 1 {
		t.Fatal("expected one alert", messages)
	}
	// A separate verified member cannot list or cancel another member's alert.
	ic := h.call(admin, "iam", "policy_get", "", "", nil)
	icfg := ic["config"].(map[string]any)
	icfg["phone_enabled"] = true
	h.call(admin, "iam", "policy_upsert", "", "", map[string]any{"config": icfg, "version": ic["version"], "reason": "测试会员隔离"})
	challenge := h.call("", "iam", "otp_send", "", "", map[string]any{"phone": "13800138077"})
	newMember := h.call("", "iam", "otp_login", "", "", map[string]any{"phone": "13800138077", "challenge_id": challenge["challenge_id"], "code": challenge["demo_code"]})
	other := platform.Text(newMember, "token")
	if platform.Int(h.call(other, "engagement", "subscriptions", "", "", nil), "total") != 0 {
		t.Fatal("cross-owner list")
	}
	h.expect(404, other, "engagement", "unsubscribe", id, "", nil)
	// Cancel/re-arm retains its business number but increments its generation.
	h.call(buyer, "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-coffee", "kind": "price", "target_cents": 20000})
	h.call(buyer, "engagement", "unsubscribe", id, "", nil)
	h.call(buyer, "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-coffee", "kind": "price", "target_cents": 19000})
	h.expect(400, admin, "engagement", "admin_cancel", id, "", nil)
	h.call(admin, "engagement", "admin_cancel", id, "", map[string]any{"reason": "客户请求停止"})
	if platform.Int(h.call(admin, "engagement", "admin_list", "", "", map[string]any{"status": "CANCELLED", "limit": 10}), "total") != 1 {
		t.Fatal("admin cancellation not visible")
	}
	// Missing stock must become sellable before a stock subscription fires.
	h.call(admin, "inventory", "adjust", "sku-headphones", "", map[string]any{"on_hand": 0, "reason": "测试缺货提醒"})
	stock := h.call(buyer, "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-headphones", "kind": "stock"})
	h.call(admin, "inventory", "adjust", "sku-headphones", "", map[string]any{"on_hand": 10, "reason": "补货"})
	config["stock_enabled"] = false
	h.call(admin, "engagement", "policy_upsert", "", "", map[string]any{"config": config, "version": 0, "reason": "暂停到货提醒"})
	if e := c.modules["engagement"].Tick(ctx); e != nil {
		t.Fatal(e)
	}
	active := h.call(buyer, "engagement", "subscriptions", "", "", map[string]any{"kind": "stock"})
	if platform.Text(mapItems(active)[0], "status") != "ACTIVE" {
		t.Fatal(active)
	}
	config["stock_enabled"] = true
	h.call(admin, "engagement", "policy_upsert", "", "", map[string]any{"config": config, "version": 1, "reason": "恢复到货提醒"})
	// Advance only the scheduled local check; no external clock sleeps.
	if e := c.dbs["engagement"].Table("engagement_subscriptions").Where("id = ?", stock["id"]).Update("next_check_at", time.Now().UTC().Add(-time.Second)).Error; e != nil {
		t.Fatal(e)
	}
	if e := c.modules["engagement"].Tick(ctx); e != nil {
		t.Fatal(e)
	}
	c.pump(t)
	triggered := h.call(buyer, "engagement", "subscriptions", "", "", map[string]any{"kind": "stock"})
	if platform.Text(mapItems(triggered)[0], "status") != "TRIGGERED" {
		t.Fatal(triggered)
	}
	// Expiration wins over a price condition and never emits a late alert.
	late := h.call(buyer, "engagement", "subscribe", "", "", map[string]any{"sku_id": "sku-watch", "kind": "price", "target_cents": 10000})
	if e := c.dbs["engagement"].Table("engagement_subscriptions").Where("id = ?", late["id"]).Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; e != nil {
		t.Fatal(e)
	}
	if e := c.modules["engagement"].Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if platform.Int(h.call(admin, "engagement", "admin_list", "", "", map[string]any{"status": "EXPIRED"}), "total") != 1 {
		t.Fatal("expired alert remained active")
	}
}
