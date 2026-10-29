package tests

import (
	"context"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCommerceUpgradeIntegration(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	login := func(name, pw string) string {
		return platform.Text(h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": pw}), "token")
	}
	admin, buyer := login("admin", "Admin@2026"), login("demo", "Lumo@2026")
	for _, service := range []string{"payment", "iam"} {
		h.expect(403, buyer, service, "policy_get", "", "", nil)
		h.expect(403, buyer, service, "policy_upsert", "", "", map[string]any{"config": map[string]any{}})
	}
	claim := func(id, typ string, amount, rate int64) string {
		h.call(admin, "coupon", "template_upsert", id, "", map[string]any{"name": id, "coupon_type": typ, "amount_cents": amount, "min_cents": 0, "discount_bps": rate, "scope_type": "all", "scope_values": []string{}, "valid_days": 7, "remaining": 5, "active": true, "expires_at": time.Now().Add(48 * time.Hour), "version": 0})
		coupon := h.call(buyer, "coupon", "claim", "", "", map[string]any{"template_id": id})
		cid := platform.Text(coupon, "id")
		if !platform.IsBusinessID(cid, "coupon") {
			t.Fatal("coupon is not snowflake", coupon)
		}
		return cid
	}
	cash, half := claim("upgrade-cash", "cash", 2000, 0), claim("upgrade-half", "discount", 0, 5000)
	input := map[string]any{"items": []map[string]any{{"sku_id": "sku-coffee", "quantity": 1}}, "points_mode": "max"}
	quote := h.call(buyer, "coupon", "calculate", "", "", input)
	if platform.Text(quote, "coupon_id") != half || platform.Int(quote, "coupon_discount_cents") != 19950 || platform.Int(quote, "points") != 3990 || platform.Int(quote, "payable_cents") != 15960 {
		t.Fatalf("best coupon/points calculation: %v", quote)
	}
	if !platform.IsBusinessID(platform.Text(quote, "quote_id"), "quote") {
		t.Fatal(quote)
	}
	if platform.Int(h.call(buyer, "points", "get", "", "", nil), "available") != 5000 {
		t.Fatal("trial pricing reserved points")
	}
	input["coupon_mode"] = "manual"
	input["coupon_id"] = cash
	input["points_mode"] = ""
	manual := h.call(buyer, "coupon", "calculate", "", "", input)
	if platform.Int(manual, "coupon_discount_cents") != 2000 || platform.Text(manual, "coupon_id") != cash {
		t.Fatal(manual)
	}
	input["coupon_mode"] = "none"
	none := h.call(buyer, "coupon", "calculate", "", "", input)
	if platform.Text(none, "coupon_id") != "" || platform.Int(none, "payable_cents") != 39900 {
		t.Fatal(none)
	}
	order := h.call(buyer, "checkout", "submit", "", "upgrade-order", map[string]any{"quote_id": quote["quote_id"], "address": map[string]any{"recipient": "测试会员", "phone": "13800138000", "address": "测试路101"}})
	oid := platform.Text(order, "order_id")
	if !platform.IsBusinessID(oid, "order") {
		t.Fatal(order)
	}
	again := h.call(buyer, "checkout", "submit", "", "upgrade-order", map[string]any{"quote_id": quote["quote_id"], "address": map[string]any{"recipient": "测试会员", "phone": "13800138000", "address": "测试路101"}})
	if again["order_id"] != oid {
		t.Fatal("retry issued a second order")
	}
	payment := h.call(buyer, "payment", "create", oid, "", nil)
	if !platform.IsBusinessID(platform.Text(payment, "id"), "payment") {
		t.Fatal(payment)
	}
	h.call(admin, "payment", "policy_upsert", "", "", map[string]any{"config": map[string]any{"demo_enabled": false}, "version": 0, "reason": "暂停渠道维护"})
	if len(mapItems(h.call("", "payment", "methods", "", "", nil))) != 0 {
		t.Fatal("disabled payment was still offered")
	}
	h.expect(409, buyer, "payment", "create", oid, "", nil)
	h.expect(409, buyer, "payment", "submit_demo", platform.Text(payment, "id"), "", nil)
	h.call(admin, "payment", "policy_upsert", "", "", map[string]any{"config": map[string]any{"demo_enabled": true}, "version": 1, "reason": "恢复支付渠道"})
	t.Setenv("DEMO_MODE", "false")
	h.expect(409, buyer, "payment", "create", oid, "", nil)
	t.Setenv("DEMO_MODE", "true")
	h.call(admin, "payment", "confirm_demo", platform.Text(payment, "id"), "", nil)
	c.pump(t)
	invoice := h.call(buyer, "invoice", "create", "", "", map[string]any{"order_id": oid, "title": "测试会员", "kind": "personal"})
	if !platform.IsBusinessID(platform.Text(invoice, "id"), "invoice") {
		t.Fatal(invoice)
	}
	application := h.call(buyer, "aftersale", "create", "", "upgrade-afs", map[string]any{"order_id": oid, "type": "refund", "reason": "测试完整退回", "lines": []map[string]any{{"sku_id": "sku-coffee", "quantity": 1}}})
	aid := platform.Text(application, "id")
	if !platform.IsBusinessID(aid, "aftersale") {
		t.Fatal(application)
	}
	h.call(admin, "aftersale", "approve", aid, "", nil)
	application = h.call(admin, "aftersale", "refund", aid, "", nil)
	rid := platform.Text(application, "refund_id")
	if !platform.IsBusinessID(rid, "refund") {
		t.Fatal(application)
	}
	h.call(admin, "refund", "confirm_demo", rid, "", nil)
	c.pump(t)
	policy := h.call(admin, "iam", "policy_get", "", "", nil)
	cfg := policy["config"].(map[string]any)
	cfg["phone_enabled"] = true
	h.call(admin, "iam", "policy_upsert", "", "", map[string]any{"config": cfg, "version": policy["version"], "reason": "开放手机验证码登录"})
	methods := h.call("", "iam", "login_methods", "", "", nil)
	if len(mapItems(methods)) != 2 {
		t.Fatal(methods)
	}
	challenge := h.call("", "iam", "otp_send", "", "", map[string]any{"phone": "13800138001"})
	code := platform.Text(challenge, "demo_code")
	if len(code) != 6 {
		t.Fatal("isolated demo must expose test code")
	}
	h.expect(429, "", "iam", "otp_send", "", "", map[string]any{"phone": "13800138001"})
	member := h.call("", "iam", "otp_login", "", "", map[string]any{"phone": "13800138001", "challenge_id": challenge["challenge_id"], "code": code})
	if !platform.IsBusinessID(platform.Text(member, "user_id"), "user") || platform.Text(member, "role") != "user" {
		t.Fatal(member)
	}
	h.call(platform.Text(member, "token"), "iam", "current", "", "", nil)
	h.expect(401, "", "iam", "otp_login", "", "", map[string]any{"phone": "13800138001", "challenge_id": challenge["challenge_id"], "code": code})
	challenge = h.call("", "iam", "otp_send", "", "", map[string]any{"phone": "13800138002"})
	bad := "000000"
	if challenge["demo_code"] == bad {
		bad = "111111"
	}
	for i := 0; i < 5; i++ {
		h.expect(401, "", "iam", "otp_login", "", "", map[string]any{"phone": "13800138002", "challenge_id": challenge["challenge_id"], "code": bad})
	}
	h.expect(401, "", "iam", "otp_login", "", "", map[string]any{"phone": "13800138002", "challenge_id": challenge["challenge_id"], "code": challenge["demo_code"]})
	cfg["password_enabled"] = false
	h.call(admin, "iam", "policy_upsert", "", "", map[string]any{"config": cfg, "version": 1, "reason": "会员改用验证码登录"})
	h.expect(403, "", "iam", "login", "", "", map[string]any{"username": "demo", "password": "Lumo@2026"})
	_ = login("admin", "Admin@2026")
	t.Setenv("QQ_LOGIN_APP_ID", "integration-app")
	t.Setenv("QQ_LOGIN_APP_SECRET", "integration-secret")
	t.Setenv("QQ_LOGIN_REDIRECT_URI", "https://shop.example/index.html")
	cfg["qq_enabled"] = true
	h.call(admin, "iam", "policy_upsert", "", "", map[string]any{"config": cfg, "version": 2, "reason": "开放QQ授权登录"})
	start := h.call("", "iam", "oauth_start", "", "", map[string]any{"provider": "qq", "proof": strings.Repeat("b", 64)})
	target, e := url.Parse(platform.Text(start, "authorization_url"))
	if e != nil || target.Host != "graph.qq.com" || target.Query().Get("client_secret") != "" {
		t.Fatal(start, e)
	}
	h.expect(401, "", "iam", "oauth_callback", "", "", map[string]any{"provider": "qq", "oauth_state": start["oauth_state"], "proof": strings.Repeat("c", 64), "code": "untrusted-code", "openid": "forged-member"})
	// Authentication configuration never affects existing authenticated sessions.
	h.call(buyer, "points", "get", "", "", nil)
	if e := c.modules["reconciliation"].Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
}
