package tests

import (
	"context"
	"encoding/json"
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

func TestHTTPReadSectionsBatchOnlyRelatedReadsAndIsolatePrincipals(t *testing.T) {
	c := newCluster(t)
	// A newly installed store may have no optional decoration document. This
	// fixture publishes a real configuration so the shell must return it.
	c.call(
		t,
		context.Background(),
		"content",
		"upsert",
		"storefront-main",
		"",
		"admin",
		"admin",
		map[string]any{"kind": "storefront", "title": "Section storefront", "body": `{"brand":"Section Mall"}`},
	)
	counted := &aggregateCountingCaller{next: c.rpc}
	server := httptest.NewServer(httpgateway.NewHandler(counted, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	readSection := func(token, section string) []map[string]any {
		t.Helper()
		return aggregateRows(t, h.call(token, "storefront", section, "", "", nil))
	}
	assertGuest := func(rows []map[string]any) {
		t.Helper()
		public := map[string]bool{
			"content":   true,
			"catalog":   true,
			"inventory": true,
			"campaign":  true,
			"flash":     true,
			"group":     true,
			"coupon":    true,
		}
		for _, row := range rows {
			aggregateData(t, row)
			if row["auth"] != false || !public[platform.Text(row, "service")] {
				t.Fatal("guest received private section data", row)
			}
		}
		for _, call := range counted.snapshot() {
			if call.command.UserID != "" || call.command.Role != "" {
				t.Fatal("public section acquired a private principal", call)
			}
		}
	}
	counted.reset()
	shell := readSection("", "shell")
	assertGuest(shell)
	if len(shell) != 2 || len(counted.snapshot()) != 2 {
		t.Fatal("shell fetched unrelated data", shell, counted.snapshot())
	}
	counted.expectCount(t, "content", "get", 2)
	counted.reset()
	alias := readSection("", "bootstrap")
	if len(alias) != 2 || len(counted.snapshot()) != 2 {
		t.Fatal("legacy alias did not stay bounded to the public shell", alias)
	}
	assertGuest(alias)

	counted.reset()
	goods := readSection("", "products")
	assertGuest(goods)
	counted.expectCount(t, "catalog", "list", 1)
	counted.expectCount(t, "inventory", "get", 0)
	counted.expectCount(t, "catalog", "get", 0)
	counted.expectCount(t, "member", "get", 0)
	counted.expectCount(t, "coupon", "templates", 0)
	products := mapItems(aggregateFragment(t, goods, "catalog", "list"))
	if len(products) != 100 {
		t.Fatalf("product section must retain the bounded catalog page: got %d", len(products))
	}
	seen := map[string]bool{}
	stocks := map[string]bool{}
	for _, product := range products {
		seen[platform.Text(product, "id")] = true
	}
	for _, product := range democatalog.Products() {
		if !seen[platform.Text(product, "id")] {
			t.Fatal("expanded demo data displaced the original selection", product)
		}
	}
	for _, row := range goods {
		if row["service"] == "inventory" {
			for _, stock := range mapItems(aggregateData(t, row)) {
				stocks[platform.Text(stock, "sku_id")] = true
			}
		}
	}
	if len(stocks) <= len(products) {
		t.Fatal("product section lost variant stock primer", len(stocks), len(products))
	}
	for _, product := range products {
		if !stocks[platform.Text(product, "id")] {
			t.Fatal("default SKU stock missing", product)
		}
		raw, _ := json.Marshal(product["variants"])
		var variants []map[string]any
		_ = json.Unmarshal(raw, &variants)
		for _, variant := range variants {
			id := platform.Text(variant, "sku_id")
			if id != "" && !stocks[id] {
				t.Fatal("variant stock missing", variant)
			}
		}
	}
	counted.expectCount(t, "inventory", "batch_get", (len(stocks)+499)/500)

	counted.reset()
	activities := readSection("", "activities")
	assertGuest(activities)
	counted.expectCount(t, "catalog", "list", 0)
	counted.expectCount(t, "coupon", "templates", 1)
	if templates := mapItems(aggregateFragment(t, activities, "coupon", "templates")); len(templates) > 2 {
		t.Fatal("activity preview loaded the full coupon center", templates)
	}
	for _, call := range counted.snapshot() {
		if call.service == "coupon" && call.command.Action == "templates" &&
			platform.Int(call.command.Data, "page_size") != 2 {
			t.Fatal("activity preview exceeded its page budget", call)
		}
	}

	login := func(name, password string) string {
		t.Helper()
		return platform.Text(
			h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": password}),
			"token",
		)
	}
	user, admin := login("demo", "Lumo@2026"), login("admin", "Admin@2026")
	h.call(user, "member", "update", "", "", map[string]any{"display_name": "Section shopper"})
	h.call(admin, "member", "update", "", "", map[string]any{"display_name": "Other account"})
	claimed := h.call(user, "coupon", "claim", "", "", map[string]any{"template_id": "welcome-100"})
	claimID := platform.Text(claimed, "id")
	previewClaim := h.call(user, "coupon", "claim", "", "", map[string]any{"template_id": "everyday-10"})
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
	// Address, orders and other detail pages stay explicit; the home summary
	// contains only its display profile, counters and favorite IDs.
	counted.reset()
	summary := h.call(user, "storefront", "account_summary", "", "", nil)
	values := platform.Map(summary["summary"])
	if platform.Text(platform.Map(values["profile"]), "display_name") != "Section shopper" {
		t.Fatal("summary used another account", summary)
	}
	if len(platform.Map(summary["errors"])) != 0 || platform.Int(values, "coupon_count") < 2 {
		t.Fatal("summary omitted actual coupon balance", summary)
	}
	counted.expectCount(t, "iam", "validate", 1)
	for _, entry := range [][2]string{{"address", "list"}, {"order", "list"}, {"aftersale", "list"}, {"invoice", "list"}, {"helpdesk", "list"}, {"interaction", "list"}} {
		counted.expectCount(t, entry[0], entry[1], 0)
	}
	for _, call := range counted.snapshot() {
		if call.service != "iam" && (call.command.UserID != "demo" || call.command.Role != "user") {
			t.Fatal("summary lost the verified principal", call)
		}
	}
	other := platform.Map(h.call(admin, "storefront", "account_summary", "", "", nil)["summary"])
	if platform.Text(platform.Map(other["profile"]), "display_name") != "Other account" {
		t.Fatal("summary reused another account's cache", other)
	}
	h.expect(401, "", "storefront", "account_summary", "", "", nil)

	profile := h.call(user, "member", "get", "", "", map[string]any{"user_id": "admin", "role": "admin"})
	if platform.Text(profile, "user_id") != "demo" {
		t.Fatal("nested data overrode verified account", profile)
	}
	addresses := mapItems(h.call(user, "address", "list", "", "", nil))
	if len(addresses) != 1 || platform.Text(addresses[0], "user_id") != "demo" {
		t.Fatal("explicit address page lost ownership", addresses)
	}
	for _, coupon := range mapItems(h.call(user, "coupon", "list", "", "", nil)) {
		if platform.Text(coupon, "user_id") != "demo" {
			t.Fatal("coupon wallet leaked another account", coupon)
		}
	}
	personalized := false
	for _, template := range mapItems(h.call(user, "coupon", "templates", "", "", map[string]any{"page": 1, "page_size": 100})) {
		if template["id"] == "welcome-100" {
			personalized = template["claimed"] == true && platform.Text(template, "coupon_id") == claimID
		}
	}
	if !personalized {
		t.Fatal("explicit coupon center lost claim personalization")
	}
	activities = readSection(user, "activities")
	personalized = false
	for _, template := range mapItems(aggregateFragment(t, activities, "coupon", "templates")) {
		if template["id"] == "everyday-10" {
			personalized = template["claimed"] == true &&
				platform.Text(template, "coupon_id") == platform.Text(previewClaim, "id")
		}
	}
	if !personalized {
		t.Fatal("activity preview lost authenticated coupon state")
	}
	counted.reset()
	anonymous := readSection("", "activities")
	assertGuest(anonymous)
	for _, template := range mapItems(aggregateFragment(t, anonymous, "coupon", "templates")) {
		if template["claimed"] != false || platform.Text(template, "coupon_id") != "" {
			t.Fatal("anonymous preview reused private coupon state", template)
		}
	}

	before := c.call(t, context.Background(), "inventory", "get", "sku-watch", "", "", "", nil)
	request := func(key, service, action, id string, data map[string]any) map[string]any {
		return map[string]any{
			"key":     key,
			"service": service,
			"action":  action,
			"command": map[string]any{"id": id, "data": data},
		}
	}
	for _, token := range []string{"", user, admin} {
		counted.reset()
		h.expect(403, token, "storefront", "batch", "", "", map[string]any{"requests": []any{
			request("valid", "inventory", "get", "sku-watch", nil),
			request("write", "inventory", "adjust", "sku-watch", map[string]any{"on_hand": 999}),
		}})
		if len(counted.snapshot()) != 0 {
			t.Fatal("removed generic batch reached a domain service", counted.snapshot())
		}
	}
	after := c.call(t, context.Background(), "inventory", "get", "sku-watch", "", "", "", nil)
	if platform.Int(after, "on_hand") != platform.Int(before, "on_hand") {
		t.Fatal("rejected batch changed inventory", before, after)
	}
	// The explicit domain batch endpoint remains fresh, deduplicated and honest
	// about missing SKUs without accepting an arbitrary service/action graph.
	checkStock := func(want int64) {
		t.Helper()
		counted.reset()
		data := h.call(
			user,
			"inventory",
			"batch_get",
			"",
			"",
			map[string]any{"ids": []string{"sku-watch", "sku-watch", "not-a-seeded-sku"}},
		)
		counted.expectCount(t, "inventory", "batch_get", 1)
		counted.expectCount(t, "inventory", "get", 0)
		items := mapItems(data)
		var missing []string
		raw, _ := json.Marshal(data["missing_ids"])
		_ = json.Unmarshal(raw, &missing)
		if len(items) != 1 || len(missing) != 1 || missing[0] != "not-a-seeded-sku" ||
			platform.Int(items[0], "on_hand") != want ||
			platform.Int(items[0], "available") != want {
			t.Fatal("fresh bulk query lost deduplication/missing-item semantics", data, want)
		}
	}
	checkStock(platform.Int(before, "on_hand"))
	fresh := platform.Int(before, "on_hand") + 23
	h.call(admin, "inventory", "adjust", "sku-watch", "section-stock-adjust", map[string]any{"on_hand": fresh})
	checkStock(fresh)
}
