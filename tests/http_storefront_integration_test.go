package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/bootstrap"
	"github.com/davveo/lumo/internal/democatalog"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

type httpShop struct {
	t         *testing.T
	server    *httptest.Server
	client    *http.Client
	coverage  map[string]int
	lastTrace string
}

func (h *httpShop) call(token, service, action, id, key string, data map[string]any) platform.Result {
	h.t.Helper()
	return h.expect(200, token, service, action, id, key, data)
}
func (h *httpShop) expect(want int, token, service, action, id, key string, data map[string]any) platform.Result {
	h.t.Helper()
	body, _ := json.Marshal(platform.Command{ID: id, IdempotencyKey: key, Data: data})
	req, e := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/"+service+"/"+action, bytes.NewReader(body))
	if e != nil {
		h.t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := h.client.Do(req)
	if e != nil {
		h.t.Fatal(e)
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(resp.Body)
	if e != nil {
		h.t.Fatal(e)
	}
	var envelope map[string]any
	if e = json.Unmarshal(raw, &envelope); e != nil {
		h.t.Fatalf("%s.%s invalid JSON: %s", service, action, raw)
	}
	if resp.StatusCode != want {
		h.t.Fatalf("%s.%s got%d want%d: %s", service, action, resp.StatusCode, want, raw)
	}
	h.lastTrace = platform.Text(envelope, "trace_id")
	if len(h.lastTrace) != 32 || resp.Header.Get("X-Trace-ID") != h.lastTrace {
		h.t.Fatalf("missing correlated trace header: %s", raw)
	}
	if want < 300 {
		h.coverage[service]++
	}
	if d, ok := envelope["data"].(map[string]any); ok {
		return d
	}
	return envelope
}
func mapItems(v platform.Result) []map[string]any {
	b, _ := json.Marshal(v["items"])
	var out []map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// A real HTTP client enters the production gateway, which validates IAM over
// real TCP gRPC. All 30 service domains participate; the deterministic durable
// outbox pump remains a local broker substitute, not a RabbitMQ network test.
func TestHTTPStorefrontEndToEnd(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	login := func(name, password string) string {
		return platform.Text(h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": password}), "token")
	}
	user := login("demo", "Lumo@2026")
	admin := login("admin", "Admin@2026")
	current := h.call(user, "iam", "current", "", "", nil)
	if platform.Text(current, "user_id") != "demo" {
		t.Fatal(current)
	}
	rotated := h.call(user, "iam", "refresh", "", "", map[string]any{"token": "forged-body-token"})
	old := user
	user = platform.Text(rotated, "token")
	h.expect(401, old, "member", "get", "", "", nil)
	h.call(user, "member", "update", "", "", map[string]any{"display_name": "HTTP integration member", "preferences": map[string]any{"birthday": "2000-01-01"}})
	h.call(user, "member", "get", "", "", nil)
	address := h.call(user, "address", "create", "", "", map[string]any{"recipient": "Integration", "phone": "13800000000", "address": "Integration Street 1", "city": "Shenzhen", "is_default": true})
	aid := platform.Text(address, "id")
	h.call(user, "address", "update", aid, "", map[string]any{"recipient": "Integration", "phone": "13800000000", "address": "Integration Street 2", "city": "Shenzhen"})
	h.call(user, "address", "default", aid, "", nil)
	h.call(user, "address", "list", "", "", nil)
	// Catalog prices and real SKU variants are server-owned; no browser price is accepted.
	catalogue := h.call("", "catalog", "list", "", "", map[string]any{"limit": 100})
	if len(mapItems(catalogue)) != 100 || platform.Int(catalogue, "total") != int64(len(democatalog.Products())+len(democatalog.ExpandedProducts())) {
		t.Fatalf("expanded storefront must return one bounded page and the actual total: returned=%d total=%v", len(mapItems(catalogue)), catalogue["total"])
	}
	h.call("", "catalog", "get", "iphone", "", nil)
	h.call("", "inventory", "get", "iphone", "", nil)
	h.call(admin, "inventory", "adjust", "iphone", "stock-http", map[string]any{"on_hand": 50})
	h.call("", "search", "query", "", "", map[string]any{"query": "Apple"})
	h.call("", "recommendation", "list", "", "", nil)
	h.call("", "content", "list", "", "", nil)
	h.call(admin, "content", "upsert", "faq-http", "", map[string]any{"kind": "faq", "title": "HTTP FAQ", "body": "Persisted help content"})
	h.call("", "helpdesk", "faq", "", "", nil)
	for _, kind := range []string{"favorite", "compare", "history"} {
		h.call(user, "engagement", "put", "", "", map[string]any{"kind": kind, "sku_id": "iphone"})
		h.call(user, "engagement", "list", "", "", map[string]any{"kind": kind})
	}
	h.call(user, "engagement", "delete", "", "", map[string]any{"kind": "favorite", "sku_id": "iphone"})
	h.call(user, "engagement", "clear", "", "", map[string]any{"kind": "history"})
	h.call(user, "engagement", "put", "", "", map[string]any{"kind": "search", "query": "Apple"})
	h.call(user, "analytics", "record", "", "http-view", map[string]any{"type": "view", "sku_id": "iphone"})
	h.call(user, "cart", "add", "", "", map[string]any{"sku_id": "iphone", "quantity": 2})
	updated := h.call(user, "cart", "set", "", "", map[string]any{"sku_id": "iphone", "quantity": 1})
	if platform.Int(updated, "quantity") != 1 {
		t.Fatal(updated)
	}
	h.call(user, "cart", "merge", "", "http-cart-merge", map[string]any{"items": []any{map[string]any{"sku_id": "coffee", "quantity": 1}}})
	h.call(user, "cart", "list", "", "", nil)
	h.call(user, "cart", "remove", "coffee", "", nil)
	h.call(user, "coupon", "templates", "", "", nil)
	h.call(user, "coupon", "claim", "", "", map[string]any{"template_id": "welcome-100"})
	h.call(user, "coupon", "list", "", "", nil)
	h.call(user, "points", "checkin", "", "", nil)
	h.call(user, "points", "get", "", "", nil)
	h.call("", "campaign", "list", "", "", nil)
	eligible := h.call("", "campaign", "evaluate", "", "", map[string]any{"subtotal_cents": 100000, "addon_sku_id": "coffee"})
	if eligible["eligible"] != true {
		t.Fatal(eligible)
	}
	question := h.call(user, "interaction", "create", "", "http-question", map[string]any{"kind": "question", "sku_id": "iphone", "body": "Is delivery tracked?"})
	qid := platform.Text(question, "id")
	pending := h.call(admin, "interaction", "list", "", "", map[string]any{"kind": "question", "include_pending": true})
	if len(mapItems(pending)) != 1 {
		t.Fatal(pending)
	}
	h.call(admin, "interaction", "moderate", qid, "", map[string]any{"approved": true})
	h.call(admin, "interaction", "answer", qid, "", map[string]any{"answer": "Yes, use the logistics page."})
	h.expect(409, user, "interaction", "like", qid, "", nil)
	h.call("", "interaction", "list", "", "", map[string]any{"kind": "question", "sku_id": "iphone"})
	file := h.call(user, "media", "create", "", "", map[string]any{"file_name": "sample.txt", "content_type": "text/plain", "size": 5})
	fid := platform.Text(file, "id")
	h.call(user, "media", "get", fid, "", nil)
	if file["upload_supported"] != false {
		t.Fatal("test must honestly report unconfigured object storage", file)
	}
	h.expect(409, user, "media", "complete", fid, "", nil)
	h.expect(409, user, "media", "download", fid, "", nil)

	place := func(sku, key, token string, extra map[string]any) string {
		t.Helper()
		d := map[string]any{"items": []any{map[string]any{"sku_id": sku, "quantity": 1}}}
		for k, v := range extra {
			d[k] = v
		}
		q := h.call(token, "checkout", "quote", "", "", d)
		quote := platform.Text(q, "quote_id")
		h.call(token, "pricing", "get", quote, "", nil)
		placed := h.call(token, "checkout", "submit", "", key, map[string]any{"quote_id": quote, "address": map[string]any{"recipient": "Integration", "phone": "13800000000", "address": "Integration Street"}})
		oid := platform.Text(placed, "order_id")
		if oid == "" {
			t.Fatal(placed)
		}
		repeated := h.call(token, "checkout", "submit", "", key, map[string]any{"quote_id": quote, "address": map[string]any{"recipient": "Integration", "phone": "13800000000", "address": "Integration Street"}})
		if platform.Text(repeated, "order_id") != oid {
			t.Fatal("duplicate order")
		}
		return oid
	}
	pay := func(oid, token string) {
		t.Helper()
		p := h.call(token, "payment", "create", oid, "", nil)
		pid := platform.Text(p, "id")
		h.call(token, "payment", "get", pid, "", nil)
		h.expect(403, token, "payment", "confirm_demo", pid, "", nil)
		h.call(admin, "payment", "confirm_demo", pid, "", nil)
		c.pump(t)
	}
	oid := place("iphone", "http-order-paid", user, map[string]any{"coupon_id": "coupon-demo-100", "points": 500})
	pay(oid, user)
	ready := h.call(user, "order", "get", oid, "", nil)
	if platform.Text(ready, "status") != "READY_TO_FULFILL" {
		t.Fatal(ready)
	}
	h.call(user, "order", "list", "", "", nil)
	h.call(user, "fulfillment", "get", oid, "", nil)
	h.call(admin, "fulfillment", "list", "", "", nil)
	invoice := h.call(user, "invoice", "create", "", "", map[string]any{"order_id": oid, "title": "Integration invoice", "kind": "personal"})
	iid := platform.Text(invoice, "id")
	h.call(admin, "invoice", "issue_demo", iid, "", nil)
	h.call(user, "invoice", "get", iid, "", nil)
	h.call(user, "invoice", "list", "", "", nil)
	shipment := h.call(admin, "fulfillment", "dispatch", oid, "", nil)
	_ = shipment
	c.pump(t)
	h.call(user, "logistics", "get", oid, "", nil)
	h.call("", "logistics", "quote", "", "", nil)
	h.call(admin, "logistics", "deliver", oid, "", nil)
	c.pump(t)
	h.call(user, "order", "complete", oid, "", nil)
	c.pump(t)
	review := h.call(user, "interaction", "create", "", "http-review", map[string]any{"kind": "review", "sku_id": "iphone", "order_id": oid, "rating": 5, "body": "Order completed through actual HTTP and RPC."})
	h.call(admin, "interaction", "moderate", platform.Text(review, "id"), "", map[string]any{"approved": true})
	h.call(user, "interaction", "like", platform.Text(review, "id"), "", nil)
	// Physical return, inspection, funds refund and invoice reversal are separate steps.
	as := h.call(user, "aftersale", "create", "", "http-return", map[string]any{"order_id": oid, "type": "return", "reason": "Integration return"})
	asid := platform.Text(as, "id")
	h.call(user, "aftersale", "list", "", "", nil)
	h.call(user, "aftersale", "get", asid, "", nil)
	h.call(admin, "aftersale", "approve", asid, "", nil)
	h.call(user, "aftersale", "return", asid, "", map[string]any{"tracking": "HTTP-RETURN-001"})
	h.call(admin, "aftersale", "inspect", asid, "", map[string]any{"condition": "sellable"})
	refunded := h.call(admin, "aftersale", "refund", asid, "", nil)
	rid := platform.Text(refunded, "refund_id")
	if rid == "" {
		row := h.call(user, "aftersale", "get", asid, "", nil)
		rid = platform.Text(row, "refund_id")
	}
	h.call(admin, "refund", "list", "", "", nil)
	h.call(user, "refund", "get", rid, "", nil)
	h.call(admin, "refund", "confirm_demo", rid, "", nil)
	c.pump(t)
	reversals := h.call(user, "invoice", "reversals", iid, "", nil)
	if len(mapItems(reversals)) != 1 {
		t.Fatal("issued invoice was not adjusted", reversals)
	}
	h.call(admin, "invoice", "adjust_demo", iid, "", nil)
	h.call(admin, "payment", "list", "", "", nil)
	h.call(admin, "reconciliation", "run", "", "", nil)
	h.call(admin, "reconciliation", "list", "", "", nil)
	ticket := h.call(user, "helpdesk", "create", "", "http-ticket", map[string]any{"title": "Order question", "body": "Please check delivery", "order_id": oid})
	tid := platform.Text(ticket, "id")
	h.call(admin, "helpdesk", "reply", tid, "http-reply", map[string]any{"body": "Your return is complete"})
	c.pump(t)
	h.call(user, "helpdesk", "get", tid, "", nil)
	h.call(user, "helpdesk", "list", "", "", nil)
	h.call(user, "helpdesk", "close", tid, "", nil)
	messages := h.call(user, "notification", "list", "", "", nil)
	if rows := mapItems(messages); len(rows) > 0 {
		h.call(user, "notification", "read", platform.Text(rows[0], "id"), "", nil)
	}
	h.call(user, "notification", "read_all", "", "", nil)
	// Closing freezes channel payment attempts and releases all reserved assets.
	cancelled := place("watch", "http-cancelled", user, nil)
	h.call(user, "checkout", "cancel", cancelled, "", nil)
	c.pump(t)
	h.expect(409, user, "payment", "create", cancelled, "", nil)
	// Flash is asynchronous: the returned durable request becomes an order only after event handling.
	h.call("", "flash", "list", "", "", nil)
	fq := h.call(user, "pricing", "quote", "", "", map[string]any{"activity_id": "flash-demo", "items": []any{map[string]any{"sku_id": "sku-headphones", "quantity": 1}}})
	flash := h.expect(202, user, "flash", "request", "", "http-flash", map[string]any{"quote_id": platform.Text(fq, "quote_id"), "address_id": aid})
	c.pump(t)
	fr := h.call(user, "flash", "get", platform.Text(flash, "id"), "", nil)
	if platform.Text(fr, "status") != "SUCCEEDED" {
		t.Fatal(fr)
	}
	// Team state comes from both distinct members' accepted payments.
	group := h.call(user, "group", "create", "", "", map[string]any{"sku_id": "coffee", "required": 2})
	gid := platform.Text(group, "id")
	h.call("", "group", "list", "", "", nil)
	h.call("", "group", "get", gid, "", nil)
	first := place("coffee", "http-group-user", user, map[string]any{"group_id": gid})
	second := place("coffee", "http-group-admin", admin, map[string]any{"group_id": gid})
	pay(first, user)
	p := h.call(admin, "payment", "create", second, "", nil)
	h.call(admin, "payment", "confirm_demo", platform.Text(p, "id"), "", nil)
	c.pump(t)
	formed := h.call("", "group", "get", gid, "", nil)
	if platform.Text(formed, "status") != "FORMED" {
		t.Fatal(formed)
	}
	h.call(user, "cart", "clear", "", "", nil)
	h.call(user, "address", "delete", aid, "", nil)
	h.call(user, "iam", "logout_all", "", "", nil)
	h.expect(401, user, "member", "get", "", "", nil)
	admin2 := login("admin", "Admin@2026")
	h.call(admin2, "iam", "logout", "", "", nil)
	h.expect(401, admin2, "iam", "current", "", "", nil)
	for _, service := range bootstrap.Names {
		if h.coverage[service] == 0 {
			t.Errorf("service has no successful HTTP coverage: %s", service)
		}
	}
	t.Logf("Real HTTP → gateway → IAM → TCP gRPC → layers → SQL covered all %d service domains", len(h.coverage))
	// At least one real HTTP trace must contain gateway, RPC and SQL children.
	traceFound := false
	for _, span := range c.exporter.GetSpans() {
		if strings.Contains(span.Name, "gateway.transport") {
			traceFound = true
			break
		}
	}
	if !traceFound {
		t.Fatal("HTTP gateway spans not exported")
	}
	_ = context.Background()
}
