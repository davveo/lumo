package tests

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

func TestAdministrativeProviderParametersAndReadableOperations(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	login := func(name, pw string) string {
		return platform.Text(h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": pw}), "token")
	}
	admin, buyer := login("admin", "Admin@2026"), login("demo", "Lumo@2026")
	for _, svc := range []string{"iam", "payment"} {
		h.expect(403, buyer, svc, "provider_get", "", "", nil)
		h.expect(403, buyer, svc, "provider_upsert", "qq", "", nil)
	}
	secret := "private-provider-secret-for-test"
	params := map[string]string{"app_id": "database-qq-app", "app_secret": secret, "redirect_uri": "https://shop.example/index.html", "scope": "get_user_info"}
	profile := h.call(admin, "iam", "provider_upsert", "qq", "", map[string]any{"values": params, "version": 0, "reason": "配置QQ回调与应用"})
	if profile["configured"] != true || platform.Int(profile, "version") != 1 {
		t.Fatal(profile)
	}
	get := h.call(admin, "iam", "provider_get", "qq", "", nil)
	raw, _ := json.Marshal(get)
	if strings.Contains(string(raw), secret) {
		t.Fatal("credentials exposed")
	}
	var payload string
	c.dbs["iam"].Table("documents").Select("payload").Where("id = ?", "login-provider-qq").Scan(&payload)
	if strings.Contains(payload, secret) || !strings.Contains(payload, "credentials_ciphertext") {
		t.Fatal("credentials not encrypted")
	}
	params["app_secret"] = ""
	params["redirect_uri"] = "https://shop.example/return.html"
	h.call(admin, "iam", "provider_upsert", "qq", "", map[string]any{"values": params, "version": 1, "reason": "只更新回调，保留密钥"})
	h.expect(409, admin, "iam", "provider_upsert", "qq", "", map[string]any{"values": params, "version": 1, "reason": "旧版本"})
	p := h.call(admin, "iam", "policy_get", "", "", nil)
	config := p["config"].(map[string]any)
	config["qq_enabled"] = true
	h.call(admin, "iam", "policy_upsert", "", "", map[string]any{"config": config, "version": p["version"], "reason": "启用已配置QQ渠道"})
	t.Setenv("QQ_LOGIN_APP_ID", "unused-environment-app")
	start := h.call("", "iam", "oauth_start", "", "", map[string]any{"provider": "qq", "proof": strings.Repeat("b", 64)})
	u, e := url.Parse(platform.Text(start, "authorization_url"))
	if e != nil || u.Query().Get("client_id") != "database-qq-app" || u.Query().Get("redirect_uri") != "https://shop.example/return.html" {
		t.Fatal("runtime did not use saved parameters", start, e)
	}
	h.expect(409, admin, "iam", "provider_upsert", "qq", "", map[string]any{"values": map[string]string{}, "clear_secrets": []string{"app_secret"}, "version": 2, "reason": "清除启用渠道密钥"})
	h.expect(400, admin, "iam", "provider_upsert", "wechat", "", map[string]any{"values": map[string]string{"redirect_uri": "http://public.example/callback"}, "version": 0, "reason": "拒绝明文回调"})
	payment := h.call(admin, "payment", "provider_upsert", "alipay", "", map[string]any{"values": map[string]string{"app_id": "alipay-app", "notify_url": "https://shop.example/pay/notify", "return_url": "https://shop.example/paid", "environment": "sandbox"}, "version": 0, "reason": "配置支付参数"})
	if platform.Int(payment, "version") != 1 || payment["adapter_ready"] != false {
		t.Fatal(payment)
	}
	h.expect(400, admin, "payment", "provider_upsert", "wechat", "", map[string]any{"values": map[string]string{"api_v3_key": "too-short"}, "version": 0, "reason": "校验密钥格式"})
	h.expect(400, admin, "payment", "provider_upsert", "alipay", "", map[string]any{"values": map[string]string{"app_private_key": "not-a-key"}, "version": 1, "reason": "校验私钥"})
	// Behavior records show the actual product/search rather than a generic kind.
	h.call(buyer, "analytics", "record", "", "readable-view", map[string]any{"type": "view", "sku_id": "sku-coffee", "page": "#/product/sku-coffee", "source": "product_detail"})
	h.call(buyer, "analytics", "record", "", "readable-search", map[string]any{"type": "search", "query": "咖啡机", "page": "#/search", "source": "search"})
	logs := h.call(admin, "analytics", "admin_list", "", "", map[string]any{"page": 1, "page_size": 1, "event_type": "view"})
	records := mapItems(logs)
	if platform.Int(logs, "total") != 1 || platform.Text(records[0], "action_label") != "浏览商品" || platform.Text(records[0], "product_name") == "" {
		t.Fatal(logs)
	}
	h.expect(403, buyer, "analytics", "admin_list", "", "", nil)
	search := h.call(admin, "analytics", "admin_list", "", "", map[string]any{"event_type": "search", "keyword": "咖啡机"})
	if platform.Int(search, "total") != 1 {
		t.Fatal(search)
	}
	if e = c.modules["analytics"].OnEvent(context.Background(), platform.Event{ID: "readable-completed", Type: "OrderCompleted", UserID: "demo", AggregateID: "ORD_123"}); e != nil {
		t.Fatal(e)
	}
	transactions := h.call(admin, "analytics", "admin_list", "", "", map[string]any{"status": "purchase_event"})
	if platform.Int(transactions, "total") != 1 || platform.Text(mapItems(transactions)[0], "action_label") != "完成订单" {
		t.Fatal(transactions)
	}
	if e = c.modules["analytics"].OnEvent(context.Background(), platform.Event{ID: "readable-refund", Type: "RefundSucceeded", UserID: "demo", AggregateID: "RFND_456", Data: map[string]any{"order_id": "ORD_123", "amount_cents": 600}}); e != nil {
		t.Fatal(e)
	}
	refunds := h.call(admin, "analytics", "admin_list", "", "", map[string]any{"event_type": "RefundSucceeded"})
	refund := mapItems(refunds)[0]
	if platform.Text(refund, "refund_id") != "RFND_456" || platform.Text(refund, "order_id") != "ORD_123" {
		t.Fatal(refunds)
	}
	// Conversation reads include the initial issue, sender, exact total and pages.
	ticket := h.call(buyer, "helpdesk", "create", "", "readable-ticket", map[string]any{"title": "配送问题", "body": "请问什么时候发货？"})
	id := platform.Text(ticket, "id")
	h.call(admin, "helpdesk", "reply", id, "reply-a", map[string]any{"body": "您好，正在安排出库。"})
	h.call(buyer, "helpdesk", "reply", id, "reply-b", map[string]any{"body": "谢谢，请帮忙跟进。"})
	conversation := h.call(admin, "helpdesk", "get", id, "", map[string]any{"message_limit": 1, "message_offset": 1})
	msgs := mapItems(platform.Result{"items": conversation["messages"]})
	if platform.Int(conversation, "message_total") != 2 || len(msgs) != 1 || platform.Text(msgs[0], "body") != "谢谢，请帮忙跟进。" || platform.Text(msgs[0], "role") != "user" {
		t.Fatal(conversation)
	}
	tickets := h.call(admin, "helpdesk", "admin_list", "", "", map[string]any{"keyword": "配送问题", "page": 1, "page_size": 1})
	if platform.Int(tickets, "total") != 1 {
		t.Fatal(tickets)
	}
	h.expect(403, buyer, "helpdesk", "admin_list", "", "", nil)
	h.call(admin, "helpdesk", "close", id, "", nil)
	h.expect(409, admin, "helpdesk", "reply", id, "closed-reply", map[string]any{"body": "不能向关闭工单追加"})
}
