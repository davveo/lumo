package tests

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/davveo/lumo/internal/democatalog"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

type aggregateObservedCall struct {
	service string
	command platform.Command
}

// Only wrap the gateway's caller: these counts measure outer domain RPCs,
// independently of legitimate downstream calls made within domain services.
type aggregateCountingCaller struct {
	next  platform.Caller
	mu    sync.Mutex
	calls []aggregateObservedCall
}

func (c *aggregateCountingCaller) Call(
	ctx context.Context,
	service string,
	command platform.Command,
) (platform.Result, error) {
	c.mu.Lock()
	c.calls = append(c.calls, aggregateObservedCall{service: service, command: command})
	c.mu.Unlock()
	return c.next.Call(ctx, service, command)
}

func (c *aggregateCountingCaller) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = nil
}

func (c *aggregateCountingCaller) snapshot() []aggregateObservedCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]aggregateObservedCall(nil), c.calls...)
}

func (c *aggregateCountingCaller) expectCount(t *testing.T, service, action string, want int) {
	t.Helper()
	got := 0
	for _, call := range c.snapshot() {
		if call.service == service && call.command.Action == action {
			got++
		}
	}
	if got != want {
		t.Fatalf("gateway %s/%s RPC count = %d, want %d", service, action, got, want)
	}
}

func aggregateRows(t *testing.T, result platform.Result) []map[string]any {
	t.Helper()
	rows := mapItems(platform.Result{"items": result["results"]})
	if len(rows) == 0 {
		t.Fatalf("missing read result envelopes: %#v", result)
	}
	return rows
}

func aggregateFragment(t *testing.T, rows []map[string]any, service, action string) platform.Result {
	t.Helper()
	for _, row := range rows {
		if row["service"] == service && row["action"] == action {
			return aggregateData(t, row)
		}
	}
	t.Fatalf("missing %s/%s fragment", service, action)
	return nil
}

func aggregateData(t *testing.T, row map[string]any) platform.Result {
	t.Helper()
	if row["error"] != nil {
		t.Fatalf("read fragment failed: %#v", row)
	}
	data, ok := row["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing fragment data: %#v", row)
	}
	return data
}

func TestHTTPReadAggregationBatchesFreshReadsAndIsolatesPrincipals(t *testing.T) {
	c := newCluster(t)
	counted := &aggregateCountingCaller{next: c.rpc}
	server := httptest.NewServer(httpgateway.NewHandler(counted, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}

	assertBootstrapCounts := func(rows []map[string]any, authenticated bool) {
		t.Helper()
		wantValidations := 0
		if authenticated {
			wantValidations = 1
		}
		counted.expectCount(t, "iam", "validate", wantValidations)
		counted.expectCount(t, "catalog", "list", 1)
		counted.expectCount(t, "inventory", "batch_get", 1)
		counted.expectCount(t, "inventory", "get", 0)
		counted.expectCount(t, "catalog", "get", 0)
		counted.expectCount(t, "coupon", "templates", 1)
		counted.expectCount(t, "recommendation", "rank_bootstrap", 1)
		counted.expectCount(t, "recommendation", "list", 0)
		flashItems := mapItems(aggregateFragment(t, rows, "flash", "list"))
		wantFlashCatalog := 0
		if len(flashItems) > 0 {
			wantFlashCatalog = 1
		}
		counted.expectCount(t, "catalog", "batch_get", wantFlashCatalog)
		for _, row := range rows {
			aggregateData(t, row)
		}
	}
	assertGuest := func(rows []map[string]any) {
		t.Helper()
		public := map[string]bool{
			"catalog":        true,
			"inventory":      true,
			"content":        true,
			"campaign":       true,
			"flash":          true,
			"group":          true,
			"coupon":         true,
			"recommendation": true,
			"storefront":     true,
		}
		for _, row := range rows {
			if row["auth"] != false || !public[platform.Text(row, "service")] ||
				(row["service"] == "coupon" && row["action"] != "templates") {
				t.Fatalf("guest received private fragment: %#v", row)
			}
		}
		for _, call := range counted.snapshot() {
			if call.service == "recommendation" && call.command.Action == "rank_bootstrap" &&
				call.command.UserID == "" &&
				call.command.Role == "system" {
				continue
			}
			// Anonymous popup decisions use trusted gateway facts and opaque
			// visitor cookies, never an authenticated shopper principal.
			if call.service == "content" && call.command.Action == "popup_decide" && call.command.UserID == "" &&
				call.command.Role == "system" {
				continue
			}
			if call.command.UserID != "" || call.command.Role != "" {
				t.Fatalf("guest RPC acquired a principal: %#v", call)
			}
		}
		for _, template := range mapItems(aggregateFragment(t, rows, "coupon", "templates")) {
			if template["claimed"] != false || platform.Text(template, "coupon_id") != "" {
				t.Fatalf("guest coupon metadata leaked a claim: %#v", template)
			}
		}
	}

	guest := aggregateRows(t, h.call("", "storefront", "bootstrap", "", "", nil))
	assertBootstrapCounts(guest, false)
	assertGuest(guest)
	products := mapItems(aggregateFragment(t, guest, "catalog", "list"))
	if len(products) != 100 {
		t.Fatalf("bootstrap must keep its bounded catalog page: got %d, want 100", len(products))
	}
	seenProducts := map[string]bool{}
	for _, product := range products {
		seenProducts[platform.Text(product, "id")] = true
	}
	for _, product := range democatalog.Products() {
		if !seenProducts[platform.Text(product, "id")] {
			t.Fatalf("expanded demo seed displaced the original storefront selection: %s", platform.Text(product, "id"))
		}
	}
	stockFragments := 0
	for _, row := range guest {
		if row["service"] == "inventory" {
			stockFragments++
		}
	}
	if stockFragments <= len(products) {
		t.Fatalf("bootstrap failed to prime variant inventory: stocks=%d products=%d", stockFragments, len(products))
	}

	login := func(name, password string) string {
		t.Helper()
		token := platform.Text(
			h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": password}),
			"token",
		)
		if token == "" {
			t.Fatal("login returned an empty token")
		}
		return token
	}
	user, admin := login("demo", "Lumo@2026"), login("admin", "Admin@2026")
	claimed := h.call(user, "coupon", "claim", "", "", map[string]any{"template_id": "welcome-100"})
	claimID := platform.Text(claimed, "id")
	if claimID == "" {
		t.Fatal("claim returned no coupon ID", claimed)
	}
	h.call(
		user,
		"address",
		"create",
		"",
		"",
		map[string]any{
			"recipient": "Aggregation demo",
			"phone":     "13800000000",
			"address":   "Integration Street",
			"city":      "Shenzhen",
		},
	)

	counted.reset()
	member := aggregateRows(t, h.call(user, "storefront", "bootstrap", "", "", nil))
	assertBootstrapCounts(member, true)
	profile := aggregateFragment(t, member, "member", "get")
	if platform.Text(profile, "user_id") != "demo" {
		t.Fatalf("bootstrap loaded another member: %#v", profile)
	}
	addresses := mapItems(aggregateFragment(t, member, "address", "list"))
	if len(addresses) != 1 || platform.Text(addresses[0], "user_id") != "demo" {
		t.Fatalf("bootstrap address ownership: %#v", addresses)
	}
	coupons := mapItems(aggregateFragment(t, member, "coupon", "list"))
	if len(coupons) < 2 {
		t.Fatal("bootstrap omitted member coupons", coupons)
	}
	for _, coupon := range coupons {
		if platform.Text(coupon, "user_id") != "demo" {
			t.Fatal("bootstrap leaked another user's coupon", coupon)
		}
	}
	personalized := false
	for _, template := range mapItems(aggregateFragment(t, member, "coupon", "templates")) {
		if platform.Text(template, "id") == "welcome-100" {
			personalized = template["claimed"] == true && platform.Text(template, "coupon_id") == claimID
		}
	}
	if !personalized {
		t.Fatal("bootstrap coupon templates lost member claim personalization")
	}
	for _, call := range counted.snapshot() {
		if call.service == "member" || call.service == "address" ||
			(call.service == "coupon" && call.command.Action == "list") {
			if call.command.UserID != "demo" || call.command.Role != "user" {
				t.Fatalf("private RPC lost authenticated principal: %#v", call)
			}
		}
	}
	// A later anonymous request must never reuse the member's personalized data.
	counted.reset()
	guest = aggregateRows(t, h.call("", "storefront", "bootstrap", "", "", nil))
	assertBootstrapCounts(guest, false)
	assertGuest(guest)

	request := func(key, service, action, id string, data map[string]any) map[string]any {
		return map[string]any{
			"key":     key,
			"service": service,
			"action":  action,
			"command": map[string]any{"id": id, "data": data},
		}
	}
	assertNoDomainCalls := func() {
		t.Helper()
		counted.expectCount(t, "iam", "validate", 1)
		if calls := counted.snapshot(); len(calls) != 1 {
			t.Fatalf("invalid batch executed domain RPCs before full preflight: %#v", calls)
		}
	}
	counted.reset()
	h.expect(400, user, "storefront", "batch", "", "", map[string]any{"requests": []any{
		map[string]any{
			"key":     "forged",
			"service": "member",
			"action":  "get",
			"command": map[string]any{"user_id": "victim", "role": "admin", "data": map[string]any{}},
		},
	}})
	assertNoDomainCalls()

	before := c.call(t, context.Background(), "inventory", "get", "sku-watch", "", "", "", nil)
	counted.reset()
	h.expect(403, admin, "storefront", "batch", "", "", map[string]any{"requests": []any{
		request("valid-first", "inventory", "get", "sku-watch", nil),
		request("forbidden-write", "inventory", "adjust", "sku-watch", map[string]any{"on_hand": 999}),
	}})
	assertNoDomainCalls()
	after := c.call(t, context.Background(), "inventory", "get", "sku-watch", "", "", "", nil)
	if platform.Int(after, "on_hand") != platform.Int(before, "on_hand") {
		t.Fatal("a rejected aggregate write changed stock", before, after)
	}

	// Repeat the same aggregate after an actual admin write. Its row must come
	// from a fresh bulk RPC, while a missing SKU remains an independent error.
	batch := map[string]any{"requests": []any{
		request("stock", "inventory", "get", "sku-watch", nil),
		request("stock-duplicate", "inventory", "get", "sku-watch", map[string]any{}),
		request("missing", "inventory", "get", "not-a-seeded-sku", nil),
		request("own-profile", "member", "get", "", map[string]any{"user_id": "victim", "role": "admin"}),
		request("own-coupons", "coupon", "list", "", nil),
	}}
	checkMixed := func(wantStock int64) {
		t.Helper()
		counted.reset()
		rows := aggregateRows(t, h.call(user, "storefront", "batch", "", "", batch))
		counted.expectCount(t, "iam", "validate", 1)
		counted.expectCount(t, "inventory", "batch_get", 1)
		counted.expectCount(t, "inventory", "get", 0)
		counted.expectCount(t, "member", "get", 1)
		counted.expectCount(t, "coupon", "list", 1)
		if len(rows) != 5 {
			t.Fatalf("mixed batch lost independent result envelopes: %#v", rows)
		}
		for _, row := range rows {
			switch row["key"] {
			case "missing":
				err := platform.Map(row["error"])
				if platform.Text(err, "code") != "NotFound" || platform.Int(err, "status") != 404 ||
					row["data"] != nil {
					t.Fatalf("missing SKU did not return an independent 404: %#v", row)
				}
			case "stock", "stock-duplicate":
				data := aggregateData(t, row)
				if platform.Int(data, "on_hand") != wantStock || platform.Int(data, "available") != wantStock {
					t.Fatalf("aggregate served stale stock: got %#v want %d", data, wantStock)
				}
			case "own-profile":
				if data := aggregateData(t, row); platform.Text(data, "user_id") != "demo" {
					t.Fatal("nested data overrode authenticated principal", data)
				}
			case "own-coupons":
				for _, coupon := range mapItems(aggregateData(t, row)) {
					if platform.Text(coupon, "user_id") != "demo" {
						t.Fatal("mixed batch exposed foreign coupons", coupon)
					}
				}
			default:
				t.Fatal("unexpected aggregate result", row)
			}
		}
	}
	checkMixed(platform.Int(before, "on_hand"))
	newStock := platform.Int(before, "on_hand") + 23
	h.call(admin, "inventory", "adjust", "sku-watch", "aggregation-stock-adjust", map[string]any{"on_hand": newStock})
	checkMixed(newStock)
}
