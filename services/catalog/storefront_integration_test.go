package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/address"
	"github.com/davveo/lumo/services/analytics"
	"github.com/davveo/lumo/services/cart"
	"github.com/davveo/lumo/services/catalog"
	"github.com/davveo/lumo/services/content"
	"github.com/davveo/lumo/services/engagement"
	"github.com/davveo/lumo/services/helpdesk"
	"github.com/davveo/lumo/services/iam"
	"github.com/davveo/lumo/services/interaction"
	"github.com/davveo/lumo/services/media"
	"github.com/davveo/lumo/services/member"
	"github.com/davveo/lumo/services/notification"
	"github.com/davveo/lumo/services/recommendation"
	"github.com/davveo/lumo/services/search"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type peers struct {
	mu      sync.Mutex
	modules map[string]platform.Module
	calls   []string
}

func (p *peers) Call(ctx context.Context, name string, c platform.Command) (platform.Result, error) {
	p.mu.Lock()
	p.calls = append(p.calls, name+"."+c.Action)
	p.mu.Unlock()
	if name == "inventory" {
		if c.Action == "batch_get" {
			var input struct {
				IDs []string `json:"ids"`
			}
			_ = platform.Decode(c.Data, &input)
			items := []platform.Result{}
			for _, id := range input.IDs {
				available := int64(50)
				if id == "sku-coffee" {
					available = 0
				}
				items = append(items, platform.Result{"sku_id": id, "available": available})
			}
			return platform.Result{"items": items, "missing_ids": []string{}}, nil
		}
		a := int64(50)
		if c.ID == "sku-coffee" {
			a = 0
		}
		return platform.Result{"sku_id": c.ID, "available": a}, nil
	}
	if name == "order" {
		owner := "demo"
		if c.ID == "other-order" {
			owner = "other"
		}
		return platform.Result{"id": c.ID, "user_id": owner, "status": "COMPLETED", "items": []map[string]any{{"sku_id": "sku-phone", "quantity": 1}}}, nil
	}
	m, ok := p.modules[name]
	if !ok {
		return nil, status.Error(codes.Unavailable, "test peer unavailable")
	}
	return m.Handle(ctx, c)
}
func setup(t *testing.T) *peers {
	t.Helper()
	t.Setenv("DEMO_MODE", "true")
	p := &peers{modules: map[string]platform.Module{}}
	factories := map[string]func(platform.Dependencies) platform.Module{"iam": iam.New, "member": member.New, "address": address.New, "catalog": catalog.New, "media": media.New, "interaction": interaction.New, "engagement": engagement.New, "search": search.New, "recommendation": recommendation.New, "content": content.New, "analytics": analytics.New, "cart": cart.New, "notification": notification.New, "helpdesk": helpdesk.New}
	for name, f := range factories {
		db, e := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), name+".db"))
		if e != nil {
			t.Fatal(e)
		}
		if e = platform.MigratePlatform(context.Background(), db); e != nil {
			t.Fatal(e)
		}
		m := f(platform.Dependencies{DB: db, RPC: p})
		if e = m.Migrate(context.Background()); e != nil {
			t.Fatal(e)
		}
		if m.Seed != nil {
			if e = m.Seed(context.Background()); e != nil {
				t.Fatal(e)
			}
		}
		p.modules[name] = m
	}
	return p
}
func cmd(action, id, user, role string, data map[string]any) platform.Command {
	return platform.Command{Action: action, ID: id, UserID: user, Role: role, Data: data}
}
func run(t *testing.T, p *peers, name string, c platform.Command) platform.Result {
	t.Helper()
	v, e := p.Call(context.Background(), name, c)
	if e != nil {
		t.Fatalf("%s.%s: %v", name, c.Action, e)
	}
	return v
}
func items(t *testing.T, v platform.Result) []map[string]any {
	t.Helper()
	var x struct {
		Items []map[string]any `json:"items"`
	}
	if e := platform.Decode(v, &x); e != nil {
		t.Fatal(e)
	}
	return x.Items
}
func TestIAMSessionLifecycle(t *testing.T) {
	p := setup(t)
	ctx := context.Background()
	if _, e := p.Call(ctx, "iam", cmd("login", "", "", "", map[string]any{"username": "demo", "password": "wrong"})); status.Code(e) != codes.Unauthenticated {
		t.Fatalf("bad password: %v", e)
	}
	v := run(t, p, "iam", cmd("login", "", "", "", map[string]any{"username": "demo", "password": "Lumo@2026"}))
	token := platform.Text(v, "token")
	if len(token) != 64 {
		t.Fatal("expected random token")
	}
	validate := cmd("validate", "", "", "", map[string]any{"token": token})
	if platform.Text(run(t, p, "iam", validate), "user_id") != "demo" {
		t.Fatal("wrong identity")
	}
	next := run(t, p, "iam", cmd("refresh", "", "", "", map[string]any{"token": token}))
	if _, e := p.Call(ctx, "iam", validate); status.Code(e) != codes.Unauthenticated {
		t.Fatal("old rotated session accepted")
	}
	raw := platform.Text(next, "token")
	run(t, p, "iam", cmd("logout", "", "demo", "user", map[string]any{"token": raw}))
	if _, e := p.Call(ctx, "iam", cmd("validate", "", "", "", map[string]any{"token": raw})); status.Code(e) != codes.Unauthenticated {
		t.Fatal("revoked session accepted")
	}
}
func TestAddressOwnershipAndDefault(t *testing.T) {
	p := setup(t)
	a := run(t, p, "address", cmd("create", "", "demo", "user", map[string]any{"recipient": "Demo", "phone": "13800000000", "address": "Test address A", "is_default": true}))
	b := run(t, p, "address", cmd("create", "", "demo", "user", map[string]any{"recipient": "Demo", "phone": "13800000000", "address": "Test address B", "is_default": true}))
	if _, e := p.Call(context.Background(), "address", cmd("get", platform.Text(a, "id"), "other", "user", nil)); status.Code(e) != codes.PermissionDenied {
		t.Fatal("cross-account address read allowed")
	}
	all := items(t, run(t, p, "address", cmd("list", "", "demo", "user", nil)))
	n := 0
	for _, v := range all {
		if v["is_default"] == true {
			n++
			if platform.Text(v, "id") != platform.Text(b, "id") {
				t.Fatal("wrong default")
			}
		}
	}
	if n != 1 {
		t.Fatalf("default count %d", n)
	}
}
func TestCartStockLimitAndIdempotentMerge(t *testing.T) {
	p := setup(t)
	run(t, p, "cart", cmd("add", "", "demo", "user", map[string]any{"sku_id": "sku-phone", "quantity": 2}))
	if _, e := p.Call(context.Background(), "cart", cmd("add", "", "demo", "user", map[string]any{"sku_id": "sku-coffee", "quantity": 1})); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("sold-out add accepted")
	}
	merge := cmd("merge", "", "demo", "user", map[string]any{"items": []map[string]any{{"sku_id": "sku-phone", "quantity": 5}}})
	merge.IdempotencyKey = "merge-one"
	run(t, p, "cart", merge)
	run(t, p, "cart", merge)
	vs := items(t, run(t, p, "cart", cmd("list", "", "demo", "user", nil)))
	if len(vs) != 1 || platform.Int(vs[0], "quantity") != 5 {
		t.Fatalf("merge duplicated %+v", vs)
	}
	if _, e := p.Call(context.Background(), "cart", cmd("add", "", "demo", "user", map[string]any{"sku_id": "sku-phone", "quantity": 6})); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("per-SKU cart limit bypass")
	}
}
func TestSearchAndRecommendationUseRPC(t *testing.T) {
	p := setup(t)
	v := run(t, p, "search", cmd("query", "", "", "", map[string]any{"query": "Apple", "in_stock": true}))
	if len(items(t, v)) != 2 {
		t.Fatalf("search results %+v", v)
	}
	r := items(t, run(t, p, "recommendation", cmd("list", "", "demo", "user", map[string]any{"order_id": "owned-order"})))
	for _, x := range r {
		if platform.Text(x, "id") == "sku-phone" || platform.Text(x, "id") == "sku-coffee" {
			t.Fatal("bought/out-of-stock recommendation")
		}
	}
	if _, e := p.Call(context.Background(), "recommendation", cmd("list", "", "demo", "user", map[string]any{"order_id": "other-order"})); status.Code(e) != codes.PermissionDenied {
		t.Fatal("cross-user order context accepted")
	}
}
func TestReviewModerationAndHelpfulDedup(t *testing.T) {
	p := setup(t)
	create := cmd("create", "", "demo", "user", map[string]any{"kind": "review", "sku_id": "sku-phone", "order_id": "owned-order", "body": "Solid product", "rating": 5})
	r := run(t, p, "interaction", create)
	id := platform.Text(r, "id")
	if len(items(t, run(t, p, "interaction", cmd("list", "", "", "", map[string]any{"sku_id": "sku-phone"})))) != 0 {
		t.Fatal("pending review leaked")
	}
	if _, e := p.Call(context.Background(), "interaction", cmd("moderate", id, "demo", "user", map[string]any{"approved": true})); status.Code(e) != codes.PermissionDenied {
		t.Fatal("user moderation allowed")
	}
	run(t, p, "interaction", cmd("moderate", id, "admin", "admin", map[string]any{"approved": true}))
	for i := 0; i < 2; i++ {
		like := run(t, p, "interaction", cmd("like", id, "demo", "user", nil))
		if platform.Int(like, "helpful_count") != 1 {
			t.Fatal("duplicate helpful vote")
		}
	}
	if len(items(t, run(t, p, "interaction", cmd("list", "", "", "", map[string]any{"sku_id": "sku-watch"})))) != 0 {
		t.Fatal("other SKU reviews leaked")
	}
}
func TestNotificationDedupReadAllAndMemberLedger(t *testing.T) {
	p := setup(t)
	ctx := context.Background()
	n := p.modules["notification"]
	for i := 0; i < 125; i++ {
		e := platform.Event{ID: fmt.Sprint("event-", i), Type: "OrderCompleted", AggregateID: "order-one", UserID: "demo", Data: map[string]any{"payable_cents": 20000}}
		if err := n.OnEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
		if err := n.OnEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	out := run(t, p, "notification", cmd("read_all", "", "demo", "user", nil))
	if platform.Int(out, "marked_read") != 125 {
		t.Fatalf("read_all only read batch: %+v", out)
	}
	e := platform.Event{ID: "growth-once", Type: "OrderCompleted", AggregateID: "order-one", UserID: "demo", Data: map[string]any{"payable_cents": 20000}}
	m := p.modules["member"]
	for i := 0; i < 2; i++ {
		if err := m.OnEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	v := run(t, p, "member", cmd("get", "", "demo", "user", nil))
	if platform.Int(v, "growth") != 2 {
		t.Fatalf("duplicate growth credit %+v", v)
	}
}
func TestHelpdeskOwnershipAndClosedReply(t *testing.T) {
	p := setup(t)
	c := cmd("create", "", "demo", "user", map[string]any{"title": "Order support", "body": "Help please", "order_id": "owned-order"})
	c.IdempotencyKey = "ticket-one"
	ticket := run(t, p, "helpdesk", c)
	id := platform.Text(ticket, "id")
	if _, e := p.Call(context.Background(), "helpdesk", cmd("get", id, "other", "user", nil)); status.Code(e) != codes.PermissionDenied {
		t.Fatal("cross-account ticket read allowed")
	}
	reply := cmd("reply", id, "admin", "admin", map[string]any{"body": "We can help"})
	reply.IdempotencyKey = "reply-one"
	run(t, p, "helpdesk", reply)
	run(t, p, "helpdesk", reply)
	v := run(t, p, "helpdesk", cmd("get", id, "demo", "user", nil))
	var conv struct {
		Messages []any `json:"messages"`
	}
	if e := platform.Decode(v, &conv); e != nil {
		t.Fatal(e)
	}
	if len(conv.Messages) != 1 {
		t.Fatal("duplicate reply")
	}
	run(t, p, "helpdesk", cmd("close", id, "demo", "user", nil))
	if _, e := p.Call(context.Background(), "helpdesk", reply); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("closed ticket reply allowed")
	}
}

func TestStorefrontLayerTraceReachesSQL(t *testing.T) {
	old := otel.GetTracerProvider()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()); otel.SetTracerProvider(old) }()
	p := setup(t)
	exporter.Reset()
	ctx, root := otel.Tracer("test").Start(context.Background(), "http.request")
	if _, e := p.Call(ctx, "catalog", cmd("get", "sku-phone", "", "", nil)); e != nil {
		t.Fatal(e)
	}
	root.End()
	spans := exporter.GetSpans()
	names := map[string]tracetest.SpanStub{}
	for _, s := range spans {
		names[s.Name] = s
		if strings.HasPrefix(s.Name, "SQL ") && strings.HasSuffix(s.Name, " catalog_records") {
			names["SQL SELECT catalog_records"] = s
		}
	}
	chain := []string{"http.request", "catalog.transport.get", "catalog.usecase.get", "catalog.repository.Get", "SQL SELECT catalog_records"}
	for i, name := range chain {
		s, ok := names[name]
		if !ok {
			t.Fatalf("missing layer span %s in %+v", name, names)
		}
		if i > 0 {
			if s.Parent.SpanID() != names[chain[i-1]].SpanContext.SpanID() {
				t.Fatalf("broken parent at %s", name)
			}
		}
	}
}

func TestCatalogAdminUpdatePreservesCreationTime(t *testing.T) {
	p := setup(t)
	first := run(t, p, "catalog", cmd("upsert", "sku-upsert-test", "admin", "admin", map[string]any{"name": "Original", "price_cents": 100, "brand": "LUMO", "active": true}))
	second := run(t, p, "catalog", cmd("upsert", "sku-upsert-test", "admin", "admin", map[string]any{"name": "Changed", "price_cents": 200, "brand": "LUMO", "active": true}))
	if platform.Text(first, "created_at") == "" || strings.HasPrefix(platform.Text(second, "created_at"), "0001-") || platform.Text(first, "created_at") != platform.Text(second, "created_at") {
		t.Fatalf("aggregate update changed or zeroed creation time: %v -> %v", first["created_at"], second["created_at"])
	}
	if platform.Text(second, "name") != "Changed" || platform.Int(second, "price_cents") != 200 {
		t.Fatal("mutable product fields not updated")
	}
}

func TestCatalogVariantsHaveAuthoritativePricesAndVisibility(t *testing.T) {
	p := setup(t)
	all := run(t, p, "catalog", cmd("list", "", "", "", map[string]any{"limit": 100}))
	if len(items(t, all)) != 12 {
		t.Fatalf("expected 12 product/default SKUs, got %+v", all)
	}
	product := run(t, p, "catalog", cmd("get", "iphone", "", "", nil))
	variant := run(t, p, "catalog", cmd("get", "iphone--1-2", "", "", nil))
	if platform.Text(variant, "product_id") != "iphone" || platform.Text(variant, "color") != "蓝色钛金属" || platform.Text(variant, "option") != "512GB" || platform.Int(variant, "price_cents") != 999900 {
		t.Fatalf("wrong variant %+v", variant)
	}
	data := map[string]any{}
	encoded, _ := json.Marshal(product)
	_ = json.Unmarshal(encoded, &data)
	data["price_cents"] = int64(809900)
	data["image"] = "https://example.test/phone.webp"
	updated := run(t, p, "catalog", cmd("upsert", "iphone", "admin", "admin", data))
	if platform.Text(updated, "image") != "https://example.test/phone.webp" {
		t.Fatal("presentation image lost")
	}
	variant = run(t, p, "catalog", cmd("get", "iphone--0-0", "", "", nil))
	if platform.Int(variant, "price_cents") != 759900 {
		t.Fatalf("variant price was not adjusted from server base %+v", variant)
	}
	data["active"] = false
	run(t, p, "catalog", cmd("upsert", "iphone", "admin", "admin", data))
	if _, e := p.Call(context.Background(), "catalog", cmd("get", "iphone--0-0", "demo", "user", nil)); status.Code(e) != codes.NotFound {
		t.Fatalf("disabled variant still purchasable: %v", e)
	}
	public := items(t, run(t, p, "catalog", cmd("list", "", "", "", map[string]any{"limit": 100})))
	if len(public) != 11 {
		t.Fatal("disabled product leaked to public list")
	}
	admin := items(t, run(t, p, "catalog", cmd("list", "", "admin", "admin", map[string]any{"limit": 100, "include_disabled": true})))
	if len(admin) != 12 {
		t.Fatal("admin cannot view disabled product")
	}
	if len(items(t, run(t, p, "catalog", cmd("list", "", "", "", map[string]any{"limit": 5, "offset": 5})))) != 5 {
		t.Fatal("product pagination includes variant rows")
	}
}
func TestContentLargeStorefrontRetainsFAQBound(t *testing.T) {
	p := setup(t)
	body := strings.Repeat("x", 30000)
	run(t, p, "content", cmd("upsert", "storefront-main", "admin", "admin", map[string]any{"kind": "storefront", "title": "Homepage", "body": body}))
	if _, e := p.Call(context.Background(), "content", cmd("upsert", "large-faq", "admin", "admin", map[string]any{"kind": "faq", "title": "FAQ", "body": body})); status.Code(e) != codes.InvalidArgument {
		t.Fatal("FAQ limit incorrectly expanded")
	}
	if _, e := p.Call(context.Background(), "content", cmd("upsert", "forbidden", "demo", "user", map[string]any{"kind": "storefront", "title": "Home", "body": "test"})); status.Code(e) != codes.PermissionDenied {
		t.Fatal("customer could publish homepage")
	}
}
func TestInteractionPendingAdminAndHelpfulReadModel(t *testing.T) {
	p := setup(t)
	c := cmd("create", "", "demo", "user", map[string]any{"kind": "question", "sku_id": "iphone--1-2", "body": "Does the 512GB model support this color?"})
	c.IdempotencyKey = "variant-question"
	q := run(t, p, "interaction", c)
	list := map[string]any{"kind": "question", "sku_id": "iphone", "include_pending": true}
	if _, e := p.Call(context.Background(), "interaction", cmd("list", "", "demo", "user", list)); status.Code(e) != codes.PermissionDenied {
		t.Fatal("customer can list others pending content")
	}
	pending := items(t, run(t, p, "interaction", cmd("list", "", "admin", "admin", list)))
	if len(pending) != 1 {
		t.Fatalf("parent product did not include variant question %+v", pending)
	}
	if len(items(t, run(t, p, "interaction", cmd("list", "", "", "", map[string]any{"kind": "question", "sku_id": "iphone"})))) != 0 {
		t.Fatal("unmoderated question leaked")
	}
	run(t, p, "interaction", cmd("answer", platform.Text(q, "id"), "admin", "admin", map[string]any{"answer": "Yes, this color is available in 512GB."}))
	published := items(t, run(t, p, "interaction", cmd("list", "", "", "", map[string]any{"kind": "question", "sku_id": "iphone"})))
	if len(published) != 1 || platform.Text(published[0], "answer") == "" {
		t.Fatal("moderated variant answer unavailable from product")
	}
}
