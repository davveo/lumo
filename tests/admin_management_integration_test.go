package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

// Management enters the authenticated HTTP gateway and the real named RPCs.
// Fixtures are confined to the harness's thirty isolated service databases.
func TestAdminManagementIntegration(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	admin := platform.Text(h.call("", "iam", "login", "", "", map[string]any{"username": "admin", "password": "Admin@2026"}), "token")
	buyer := platform.Text(h.call("", "iam", "login", "", "", map[string]any{"username": "demo", "password": "Lumo@2026"}), "token")
	ctx := context.Background()
	run := func(name string, f func(*testing.T)) {
		t.Run(name, func(st *testing.T) { h.t = st; defer func() { h.t = t }(); f(st) })
	}
	equal := func(got, want any) {
		h.t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			h.t.Fatalf("got %v; want %v", got, want)
		}
	}
	config := func(service string) map[string]any {
		t.Helper()
		return h.call(admin, service, "policy_get", "", "", nil)["config"].(map[string]any)
	}
	savePolicy := func(service string, p map[string]any, version int64) platform.Result {
		t.Helper()
		return h.call(admin, service, "policy_upsert", "", "", map[string]any{"config": p, "version": version, "reason": "集成验证规则"})
	}
	for service, actions := range map[string][]string{
		"flash": {"admin_list", "upsert", "requests_admin"}, "group": {"admin_list", "rules_admin", "rule_upsert"}, "campaign": {"rule_upsert"},
		"catalog": {"admin_list"}, "inventory": {"admin_list", "threshold", "ledger"}, "points": {"admin_get", "adjust", "ledger", "policy_upsert"},
		"member": {"policy_upsert"}, "logistics": {"policy_get", "policy_upsert"}, "content": {"admin_list", "admin_upsert"},
		"notification": {"templates_admin", "template_upsert"}, "recommendation": {"policy_get", "policy_upsert"}, "search": {"policy_get", "policy_upsert"},
		"iam": {"audit_list", "admin_get"}, "reconciliation": {"admin_list", "get", "note"}, "management": {"tasks", "retry"},
	} {
		for _, action := range actions {
			h.expect(403, buyer, service, action, "", "", map[string]any{"role": "admin", "user_id": "admin"})
		}
	}

	run("global inventory search, warning thresholds and untruncated history", func(t *testing.T) {
		for i := 0; i < 25; i++ {
			h.call(admin, "catalog", "upsert", fmt.Sprintf("mg-disabled-%02d", i), "", map[string]any{"name": fmt.Sprintf("运营全局商品 %02d", i), "price_cents": 10000, "category": "数码", "brand": "管理测试", "active": false})
		}
		page := h.call(admin, "catalog", "admin_list", "", "", map[string]any{"keyword": "运营全局", "page": 4, "page_size": 8})
		equal(platform.Int(page, "total"), 25)
		equal(len(mapItems(page)), 1)
		equal(mapItems(page)[0]["active"], false)
		equal(platform.Int(h.call(admin, "catalog", "admin_list", "", "", map[string]any{"keyword": "%"}), "total"), 0)
		stock := h.call(admin, "inventory", "get", "sku-watch", "", nil)
		threshold := platform.Int(stock, "threshold")
		h.call(admin, "inventory", "threshold", "sku-watch", "", map[string]any{"threshold": 99999, "expected_threshold": threshold, "reason": "提前备货预警"})
		h.expect(409, admin, "inventory", "threshold", "sku-watch", "", map[string]any{"threshold": 1, "expected_threshold": threshold, "reason": "过期页面"})
		warning := h.call(admin, "inventory", "admin_list", "", "", map[string]any{"status": "low", "keyword": "sku-watch", "page_size": 1})
		equal(platform.Int(warning, "total"), 1)
		equal(mapItems(warning)[0]["sku_id"], "sku-watch")
		onhand := platform.Int(stock, "on_hand")
		for i := int64(1); i <= 23; i++ {
			h.call(admin, "inventory", "adjust", "sku-watch", fmt.Sprintf("mg-stock-%d", i), map[string]any{"on_hand": onhand + i, "expected_on_hand": onhand + i - 1, "reason": "盘点流水验证"})
		}
		history := h.call(admin, "inventory", "adjustments", "sku-watch", "", map[string]any{"page": 3, "page_size": 10})
		equal(platform.Int(history, "total"), 23)
		equal(len(mapItems(history)), 3)
		c.call(t, ctx, "inventory", "reserve", "mg-reservation", "", "system", "demo", map[string]any{"order_id": "mg-order", "lines": []map[string]any{{"sku_id": "sku-watch", "quantity": 1}}})
		c.call(t, ctx, "inventory", "cancel", "mg-reservation", "", "system", "demo", nil)
		ledger := h.call(admin, "inventory", "ledger", "sku-watch", "", map[string]any{"page_size": 1})
		if platform.Int(ledger, "total") < 2 {
			t.Fatalf("missing stock movement ledger: %v", ledger)
		}
	})

	run("custom flash sessions and immutable group rule snapshots", func(t *testing.T) {
		quota := func(id, kind string) {
			h.call(admin, "campaign", "rule_upsert", id, "", map[string]any{"name": "管理活动配额", "kind": kind, "discount_cents": 1000, "quota": 100, "min_quantity": 1, "active": true, "version": 0, "reason": "创建专用额度"})
		}
		quota("mg-flash-quota", constants.CampaignKindFlash)
		quota("mg-group-quota", constants.CampaignKindGroup)
		session := map[string]any{"name": "自定义秒杀", "sku_id": "sku-coffee", "activity_id": "mg-flash-quota", "active": true, "version": 0, "reason": "配置新场次", "starts_at": time.Now().Add(time.Hour), "ends_at": time.Now().Add(2 * time.Hour)}
		h.call(admin, "flash", "upsert", "mg-flash", "", session)
		equal(h.call("", "flash", "session_get", "mg-flash", "", nil)["open"], false)
		h.expect(409, buyer, "pricing", "quote", "", "", map[string]any{"items": []map[string]any{{"sku_id": "sku-coffee", "quantity": 1}}, "activity_id": "mg-flash-quota"})
		session["version"] = 1
		session["starts_at"] = time.Now().Add(-time.Hour)
		h.call(admin, "flash", "upsert", "mg-flash", "", session)
		h.expect(409, admin, "flash", "upsert", "mg-flash", "", session)
		quote := h.call(buyer, "pricing", "quote", "", "", map[string]any{"items": []map[string]any{{"sku_id": "sku-coffee", "quantity": 1}}, "activity_id": "mg-flash-quota"})
		equal(platform.Int(quote, "campaign_discount_cents"), 1000)
		request := h.expect(202, buyer, "flash", "request", "", "mg-flash-request", map[string]any{"quote_id": quote["quote_id"], "address": map[string]any{"recipient": "测试会员", "phone": "13800138000", "address": "测试街道101"}})
		equal(platform.Text(request, "session_id"), "mg-flash")
		equal(platform.Int(h.call(admin, "flash", "requests_admin", "", "", map[string]any{"keyword": "mg-flash", "page_size": 10}), "total"), 1)
		h.expect(409, buyer, "pricing", "quote", "", "", map[string]any{"items": []map[string]any{{"sku_id": "sku-phone", "quantity": 1}}, "activity_id": "mg-flash-quota"})
		rule := map[string]any{"name": "三人限时团", "sku_id": "sku-headphones", "activity_id": "mg-group-quota", "required": 3, "duration_minutes": 45, "active": true, "version": 0, "reason": "创建新规则"}
		h.call(admin, "group", "rule_upsert", "mg-group-rule", "", rule)
		group := h.call(buyer, "group", "create", "", "mg-group-create", map[string]any{"rule_id": "mg-group-rule", "sku_id": "sku-headphones", "required": 9, "duration_minutes": 999})
		equal(platform.Int(group, "required"), 3)
		equal(platform.Int(group, "rule_version"), 1)
		expires, _ := time.Parse(time.RFC3339Nano, platform.Text(group, "expires_at"))
		if time.Until(expires) < 44*time.Minute || time.Until(expires) > 46*time.Minute {
			t.Fatal(group)
		}
		rule["version"] = 1
		rule["required"] = 4
		rule["active"] = false
		h.call(admin, "group", "rule_upsert", "mg-group-rule", "", rule)
		retried := h.call(buyer, "group", "create", "", "mg-group-create", map[string]any{"rule_id": "mg-group-rule", "sku_id": "sku-headphones"})
		equal(retried["id"], group["id"])
		equal(platform.Int(retried, "required"), 3)
		h.expect(409, buyer, "group", "create", "", "mg-new-group", map[string]any{"rule_id": "mg-group-rule", "sku_id": "sku-headphones"})
		quote = h.call(buyer, "pricing", "quote", "", "", map[string]any{"items": []map[string]any{{"sku_id": "sku-headphones", "quantity": 1}}, "group_id": group["id"]})
		equal(platform.Text(quote, "activity_id"), "mg-group-quota")
		equal(platform.Int(quote, "campaign_discount_cents"), 1000)
	})

	run("member compensation and immutable settlement policies", func(t *testing.T) {
		h.expect(404, admin, "points", "admin_get", "missing-member", "", nil)
		h.expect(409, admin, "points", "adjust", "admin", "bad-target", map[string]any{"amount": 10, "expected_available": 0, "reason": "不应调整管理员"})
		account := h.call(admin, "points", "admin_get", "demo", "", nil)
		balance := platform.Int(account, "available")
		compensation := map[string]any{"amount": 123, "expected_available": balance, "reason": "客服积分补偿"}
		equal(platform.Int(h.call(admin, "points", "adjust", "demo", "mg-compensation", compensation), "available"), balance+123)
		equal(platform.Int(h.call(admin, "points", "adjust", "demo", "mg-compensation", compensation), "available"), balance+123)
		h.expect(409, admin, "points", "adjust", "demo", "mg-stale", map[string]any{"amount": 1, "expected_available": balance, "reason": "旧余额"})
		h.expect(409, admin, "points", "adjust", "demo", "mg-compensation", map[string]any{"amount": 124, "expected_available": balance, "reason": "客服积分补偿"})
		ledger := h.call(admin, "points", "ledger", "demo", "", map[string]any{"status": constants.PointsLedgerKindManual})
		equal(platform.Int(ledger, "total"), 1)
		equal(mapItems(ledger)[0]["actor_id"], "admin")
		h.call(admin, "member", "update", "demo", "", map[string]any{"display_name": "后台维护会员", "avatar": "", "reason": "更正会员资料"})
		equal(platform.Text(h.call(buyer, "member", "get", "", "", nil), "display_name"), "后台维护会员")
		p := config("points")
		p["checkin_points"] = 37
		p["deduction_bps"] = 2000
		p["reward_per_yuan"] = 2
		savePolicy("points", p, 0)
		h.expect(409, admin, "points", "policy_upsert", "", "", map[string]any{"config": p, "version": 0, "reason": "旧配置"})
		m := config("member")
		m["gold_growth"] = 10
		m["growth_spend_cents"] = 1000
		savePolicy("member", m, 0)
		before := h.call(buyer, "points", "get", "", "", nil)
		h.call(buyer, "points", "checkin", "", "", nil)
		after := h.call(buyer, "points", "get", "", "", nil)
		equal(platform.Int(after, "available")-platform.Int(before, "available"), 37)
		quote := h.call(buyer, "pricing", "quote", "", "", map[string]any{"items": []map[string]any{{"sku_id": "sku-coffee", "quantity": 1}}, "points": 5000})
		equal(platform.Int(quote, "points_limit_cents"), 7980)
		equal(platform.Int(quote, "reward_points"), 698)
		equal(platform.Int(quote, "growth_points"), 34)
		p["deduction_bps"] = 0
		p["reward_per_yuan"] = 0
		savePolicy("points", p, 1)
		order := h.call(buyer, "checkout", "submit", "", "mg-policy-snapshot", map[string]any{"quote_id": quote["quote_id"], "address": map[string]any{"recipient": "测试会员", "phone": "13800138000", "address": "规则验证街道101"}})
		stored := h.call(buyer, "order", "get", platform.Text(order, "order_id"), "", nil)
		equal(platform.Int(stored, "payable_cents"), 34900)
		snapshot := stored["quote"].(map[string]any)
		equal(platform.Int(snapshot, "reward_points"), 698)
		equal(platform.Int(snapshot, "growth_points"), 34)
		payment := h.call(buyer, "payment", "create", platform.Text(order, "order_id"), "", nil)
		h.call(admin, "payment", "confirm_demo", platform.Text(payment, "id"), "", nil)
		c.pump(t)
		h.call(admin, "fulfillment", "dispatch", platform.Text(order, "order_id"), "", nil)
		c.pump(t)
		beforeReward := h.call(buyer, "points", "get", "", "", nil)
		h.call(buyer, "order", "complete", platform.Text(order, "order_id"), "", nil)
		c.pump(t)
		equal(platform.Int(h.call(buyer, "points", "get", "", "", nil), "available")-platform.Int(beforeReward, "available"), 698)
		profile := h.call(buyer, "member", "get", "", "", nil)
		equal(platform.Int(profile, "growth"), 34)
		equal(platform.Text(profile, "level"), constants.MemberLevelGold)
	})

	run("shipping regional fees are charged by checkout", func(t *testing.T) {
		policy := config("logistics")
		policy["mode"] = "per_item"
		policy["base_fee_cents"] = 600
		policy["additional_fee_cents"] = 200
		policy["blocked_regions"] = []string{"西藏"}
		policy["regions"] = []map[string]any{{"region": "广东省", "base_fee_cents": 800, "additional_fee_cents": 300, "free_threshold_cents": 0}}
		savePolicy("logistics", policy, 0)
		address := h.call(buyer, "address", "create", "", "", map[string]any{"recipient": "测试会员", "phone": "13800138000", "address": "科技园101", "province": "广东省", "city": "深圳市", "district": "南山区"})
		quote := h.call(buyer, "checkout", "quote", "", "", map[string]any{"items": []map[string]any{{"sku_id": "sku-coffee", "quantity": 2}}, "address_id": address["id"]})
		equal(platform.Int(quote, "shipping_cents"), 1100)
		equal(platform.Int(quote, "payable_cents"), 80900)
		other := h.call(buyer, "address", "create", "", "", map[string]any{"recipient": "测试会员", "phone": "13800138000", "address": "新地址101", "province": "浙江省", "city": "杭州市", "district": "西湖区"})
		h.expect(409, buyer, "checkout", "submit", "", "mg-shipping-mismatch", map[string]any{"quote_id": quote["quote_id"], "address_id": other["id"]})
		policy["base_fee_cents"] = 1200
		savePolicy("logistics", policy, 1)
		order := h.call(buyer, "checkout", "submit", "", "mg-shipping-snapshot", map[string]any{"quote_id": quote["quote_id"], "address_id": address["id"]})
		equal(platform.Int(h.call(buyer, "order", "get", platform.Text(order, "order_id"), "", nil), "payable_cents"), 80900)
		h.expect(409, buyer, "pricing", "quote", "", "", map[string]any{"items": []map[string]any{{"sku_id": "sku-coffee", "quantity": 1}}, "address": map[string]any{"province": "西藏自治区"}})
	})

	run("scheduled content, notification templates and discovery policies", func(t *testing.T) {
		article := map[string]any{"kind": "notice", "title": "定时运营公告", "body": "公告正文", "status": constants.ContentStatusDraft, "version": 0, "reason": "内容草稿"}
		h.call(admin, "content", "admin_upsert", "mg-notice", "", article)
		h.expect(404, "", "content", "get", "mg-notice", "", nil)
		article["version"] = 1
		article["status"] = constants.ContentStatusScheduled
		article["publish_at"] = time.Now().Add(time.Hour)
		h.call(admin, "content", "admin_upsert", "mg-notice", "", article)
		h.expect(404, "", "content", "get", "mg-notice", "", nil)
		article["version"] = 2
		article["publish_at"] = time.Now().Add(-time.Hour)
		h.call(admin, "content", "admin_upsert", "mg-notice", "", article)
		equal(platform.Text(h.call("", "content", "get", "mg-notice", "", nil), "title"), "定时运营公告")
		h.expect(409, admin, "content", "admin_upsert", "mg-notice", "", article)
		article["version"] = 3
		article["status"] = constants.ContentStatusPaused
		h.call(admin, "content", "admin_upsert", "mg-notice", "", article)
		h.expect(404, "", "content", "get", "mg-notice", "", nil)
		h.expect(400, admin, "content", "admin_upsert", "storefront-categories", "", article)
		h.call(admin, "notification", "template_upsert", constants.EventInvoiceIssued, "", map[string]any{"title": "票据 {{business_id}} 已开具", "body": "业务金额 {{amount}} 元", "enabled": true, "version": 0, "reason": "票据文案调整"})
		equal(platform.Int(h.call(admin, "notification", "templates_admin", "", "", map[string]any{"keyword": "InvoiceIssued"}), "total"), 1)
		event := platform.Event{ID: "mg-notice-event", Type: constants.EventInvoiceIssued, AggregateID: "mg-invoice", UserID: "demo", Data: map[string]any{"amount_cents": 12345}}
		if e := c.modules["notification"].OnEvent(ctx, event); e != nil {
			t.Fatal(e)
		}
		rows := mapItems(h.call(buyer, "notification", "list", "", "", nil))
		found := false
		for _, row := range rows {
			if platform.Text(row, "business_id") == "mg-invoice" || strings.Contains(platform.Text(row, "title"), "mg-invoice") {
				equal(row["title"], "票据 mg-invoice 已开具")
				equal(row["body"], "业务金额 123.45 元")
				found = true
			}
		}
		if !found {
			t.Fatalf("template not projected: %v", rows)
		}
		h.call(admin, "notification", "template_upsert", constants.EventInvoiceIssued, "", map[string]any{"title": "停用票据通知", "body": "业务编号 {{business_id}}", "enabled": false, "version": 1, "reason": "暂停通知"})
		event.ID = "mg-suppressed-event"
		event.AggregateID = "mg-suppressed"
		if e := c.modules["notification"].OnEvent(ctx, event); e != nil {
			t.Fatal(e)
		}
		for _, row := range mapItems(h.call(buyer, "notification", "list", "", "", nil)) {
			if strings.Contains(platform.Text(row, "body"), "mg-suppressed") {
				t.Fatal("disabled template projected a notification")
			}
		}
		recommendation := config("recommendation")
		recommendation["pinned_ids"] = []string{"coffee"}
		recommendation["excluded_ids"] = []string{"iphone"}
		savePolicy("recommendation", recommendation, 0)
		rec := h.call("", "recommendation", "list", "", "", map[string]any{"scene": "home", "limit": 20})
		items := mapItems(rec)
		equal(items[0]["id"], "coffee")
		for _, row := range items {
			if row["id"] == "iphone" {
				t.Fatal("excluded recommendation leaked")
			}
		}
		search := config("search")
		search["excluded_ids"] = []string{"coffee"}
		search["default_sort"] = "price_desc"
		search["max_results"] = 100
		savePolicy("search", search, 0)
		for _, row := range mapItems(h.call("", "search", "query", "", "", map[string]any{"query": "咖啡"})) {
			if row["id"] == "coffee" {
				t.Fatal("excluded search result leaked")
			}
		}
	})

	run("asynchronous period reconciliation crosses page boundaries", func(t *testing.T) {
		now := time.Now().UTC().Add(-time.Minute)
		for i := 0; i < 105; i++ {
			if e := c.dbs["payment"].Table("payments").Create(map[string]any{"id": fmt.Sprintf("mg-pay-%03d", i), "order_id": fmt.Sprintf("mg-missing-order-%03d", i), "user_id": "demo", "amount": 100, "status": constants.PaymentStatusSucceeded, "created_at": now, "updated_at": now}).Error; e != nil {
				t.Fatal(e)
			}
		}
		day := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
		run := h.call(admin, "reconciliation", "run", "", "mg-async-recon", map[string]any{"from": day, "to": day, "async": true, "reason": "日常账期核对"})
		id := platform.Text(run, "id")
		equal(platform.Text(run, "status"), constants.ReconciliationStatusQueued)
		equal(h.call(admin, "reconciliation", "run", "", "mg-async-recon", map[string]any{"from": day, "to": day, "async": true, "reason": "日常账期核对"})["id"], id)
		for i := 0; i < 5; i++ {
			if e := c.modules["reconciliation"].Tick(ctx); e != nil {
				t.Fatal(e)
			}
		}
		report := h.call(admin, "reconciliation", "get", id, "", map[string]any{"page": 11, "page_size": 10})
		equal(platform.Text(report, "status"), constants.ReconciliationStatusDifferences)
		if platform.Int(report, "checked") < 105 {
			t.Fatal(report)
		}
		diffs := report["differences"].(map[string]any)
		equal(platform.Int(diffs, "total"), 105)
		equal(len(mapItems(diffs)), 5)
		note := map[string]any{"body": "已联系支付渠道核对"}
		h.call(admin, "reconciliation", "note", id, "mg-note", note)
		h.call(admin, "reconciliation", "note", id, "mg-note", note)
		report = h.call(admin, "reconciliation", "get", id, "", nil)
		equal(len(report["notes"].([]any)), 1)
		if e := c.dbs["reconciliation"].Table("runs").Where("id = ?", id).Update("status", constants.ReconciliationStatusFailed).Error; e != nil {
			t.Fatal(e)
		}
		h.call(admin, "reconciliation", "run", id, "mg-resume", map[string]any{"retry": true, "reason": "渠道已恢复"})
		for i := 0; i < 2; i++ {
			if e := c.modules["reconciliation"].Tick(ctx); e != nil {
				t.Fatal(e)
			}
		}
		equal(platform.Int(h.call(admin, "reconciliation", "get", id, "", nil), "difference_count"), 105)
	})

	run("per service event recovery respects leases, deduplication and audit", func(t *testing.T) {
		event := platform.Event{ID: "mg-replay-invoice", Type: constants.EventInvoiceIssued, AggregateID: "mg-recovery", UserID: "demo", Data: map[string]any{"amount_cents": 100}}
		raw, _ := json.Marshal(event)
		if e := c.dbs["notification"].Create(&platform.Inbox{Consumer: "notification", EventID: event.ID, State: constants.InboxStateDead, PayloadJSON: string(raw)}).Error; e != nil {
			t.Fatal(e)
		}
		tasks := h.call(admin, "management", "tasks", "", "", map[string]any{"service": "notification", "kind": "inbox", "keyword": event.ID})
		equal(platform.Int(tasks, "total"), 1)
		equal(mapItems(tasks)[0]["retryable"], true)
		if _, ok := mapItems(tasks)[0]["payload_json"]; ok {
			t.Fatal("management exposed event payload")
		}
		data := map[string]any{"service": "notification", "kind": "inbox", "reason": "依赖恢复后重试"}
		h.call(admin, "management", "retry", event.ID, "mg-replay", data)
		h.call(admin, "management", "retry", event.ID, "mg-replay", data)
		if e := c.modules["notification"].Tick(ctx); e != nil {
			t.Fatal(e)
		}
		tasks = h.call(admin, "management", "tasks", "", "", map[string]any{"service": "notification", "kind": "inbox", "keyword": event.ID})
		equal(mapItems(tasks)[0]["state"], constants.InboxStateDone)
		h.expect(409, admin, "management", "retry", event.ID, "mg-replay-again", data)
		equal(platform.Int(h.call(admin, "management", "tasks", "", "", map[string]any{"service": "notification", "kind": "audit", "keyword": event.ID}), "total"), 1)
		event.ID = "mg-leased-event"
		raw, _ = json.Marshal(event)
		if e := c.dbs["notification"].Create(&platform.Inbox{Consumer: "notification", EventID: event.ID, State: constants.InboxStateProcessing, PayloadJSON: string(raw), LeaseUntil: time.Now().Add(time.Minute)}).Error; e != nil {
			t.Fatal(e)
		}
		h.expect(409, admin, "management", "retry", event.ID, "mg-leased-replay", data)
		h.expect(400, admin, "management", "tasks", "", "", map[string]any{"service": "unknown", "kind": "inbox"})
		for _, service := range []string{"notification", "inventory", "points", "logistics"} {
			h.call(admin, "management", "tasks", "", "", map[string]any{"service": service, "kind": "audit"})
		}
		h.call(admin, "iam", "set_active", "demo", "", map[string]any{"active": false, "expected_active": true, "reason": "审计验证"})
		audit := h.call(admin, "iam", "audit_list", "demo", "", map[string]any{"page_size": 10})
		equal(platform.Int(audit, "total"), 1)
		equal(mapItems(audit)[0]["actor_id"], "admin")
	})
}
