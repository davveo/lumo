package recommendation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	recommendationmod "github.com/davveo/lumo/services/recommendation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type observedRecommendationCall struct {
	service string
	command platform.Command
}
type personalizedCaller struct {
	mu             sync.Mutex
	calls          []observedRecommendationCall
	base, recall   []platform.Result
	recallDegraded bool
	metadata       map[string]platform.Result
	signals        map[string]map[string][]platform.Result
	orders         map[string][]platform.Result
	contextOrder   platform.Result
	missing        map[string]bool
	fail           map[string]error
}

func (f *personalizedCaller) Call(ctx context.Context, target string, c platform.Command) (platform.Result, error) {
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	f.mu.Lock()
	f.calls = append(f.calls, observedRecommendationCall{target, c})
	f.mu.Unlock()
	if err := f.fail[target+"/"+c.Action]; err != nil {
		return nil, err
	}
	switch target + "/" + c.Action {
	case "catalog/list":
		return platform.Result{"items": f.base}, nil
	case "catalog/recommendation_candidates":
		if c.Role != "system" || c.UserID != "" {
			return nil, status.Error(codes.PermissionDenied, "internal recall identity")
		}
		return platform.Result{"items": f.recall, "degraded": f.recallDegraded}, nil
	case "catalog/batch_get":
		var data struct {
			IDs []string `json:"ids"`
		}
		_ = platform.Decode(c.Data, &data)
		rows := []platform.Result{}
		for _, id := range data.IDs {
			if row, ok := f.metadata[id]; ok {
				rows = append(rows, row)
			}
		}
		return platform.Result{"items": rows}, nil
	case "engagement/list":
		if c.Role != "user" {
			return nil, status.Error(codes.PermissionDenied, "profile must be owner-scoped")
		}
		return platform.Result{"items": f.signals[c.UserID][platform.Text(c.Data, "kind")]}, nil
	case "order/list":
		if c.Role != "user" {
			return nil, status.Error(codes.PermissionDenied, "orders must be owner-scoped")
		}
		return platform.Result{"items": f.orders[c.UserID]}, nil
	case "order/get":
		return f.contextOrder, nil
	case "inventory/batch_get":
		var data struct {
			IDs []string `json:"ids"`
		}
		_ = platform.Decode(c.Data, &data)
		rows := []platform.Result{}
		for _, id := range data.IDs {
			if !f.missing[id] {
				rows = append(rows, platform.Result{"sku_id": id, "available": 10, "on_hand": 10, "reserved": 0, "allocated": 0})
			}
		}
		return platform.Result{"items": rows}, nil
	}
	return nil, status.Error(codes.Unimplemented, "unexpected recommendation peer")
}
func (f *personalizedCaller) count(service, action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.service == service && c.command.Action == action {
			n++
		}
	}
	return n
}
func (f *personalizedCaller) reset() { f.mu.Lock(); defer f.mu.Unlock(); f.calls = nil }
func recoProduct(id, name, category, brand string, price int) platform.Result {
	return platform.Result{"id": id, "name": name, "active": true, "status": "ACTIVE", "price_cents": price, "attributes": map[string]any{"category": category, "brand": brand, "image": "coffee.jpg"}, "created_at": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)}
}
func recoSignal(owner, id string, age time.Duration) platform.Result {
	return platform.Result{"user_id": owner, "sku_id": id, "updated_at": time.Now().Add(-age).UTC().Format(time.RFC3339)}
}
func personalSetup(t *testing.T) (platform.Module, *personalizedCaller) {
	t.Helper()
	db, err := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "personal.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { _ = sql.Close() })
	f := &personalizedCaller{metadata: map[string]platform.Result{}, signals: map[string]map[string][]platform.Result{}, orders: map[string][]platform.Result{}, missing: map[string]bool{}, fail: map[string]error{}}
	f.base = []platform.Result{recoProduct("phone", "无线降噪耳机", "手机数码", "Sony", 100000), recoProduct("coffee", "香醇咖啡豆", "食品生鲜", "LUMO", 10000), recoProduct("sofa", "布艺沙发", "家居生活", "HOME", 200000)}
	for _, p := range f.base {
		f.metadata[platform.Text(p, "id")] = p
	}
	f.metadata["seed-phone"] = recoProduct("seed-phone", "无线耳机", "手机数码", "Sony", 100000)
	f.metadata["seed-coffee"] = recoProduct("seed-coffee", "精品咖啡", "食品生鲜", "LUMO", 10000)
	m := recommendationmod.New(platform.Dependencies{DB: db, RPC: f})
	if err = m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = m.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m, f
}
func recoList(t *testing.T, m platform.Module, user string, data map[string]any) platform.Result {
	t.Helper()
	r, err := m.Handle(context.Background(), platform.Command{Action: "list", UserID: user, Role: "user", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func recoIDs(t *testing.T, r platform.Result) []string {
	t.Helper()
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if err := platform.Decode(r, &decoded); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, p := range decoded.Items {
		ids = append(ids, platform.Text(p, "id"))
	}
	return ids
}

func TestRecommendationRealUserPreferencesChangeRankingAndCannotBeInjected(t *testing.T) {
	m, f := personalSetup(t)
	f.signals["alice"] = map[string][]platform.Result{"favorite": {recoSignal("alice", "seed-phone", 0)}}
	f.signals["bob"] = map[string][]platform.Result{"favorite": {recoSignal("bob", "seed-coffee", 0)}}
	a := recoList(t, m, "alice", map[string]any{"limit": 2})
	b := recoList(t, m, "bob", map[string]any{"limit": 2})
	if recoIDs(t, a)[0] != "phone" || recoIDs(t, b)[0] != "coffee" {
		t.Fatalf("profiles did not change ranking: alice=%v bob=%v", a, b)
	}
	if a["personalized"] != true || a["personalized_model"] != false || a["strategy"] != "hybrid_content_v2" {
		t.Fatalf("dishonest/missing strategy: %v", a)
	}
	injected := recoList(t, m, "bob", map[string]any{"user_id": "alice", "signals": map[string]any{"favorite": []platform.Result{recoSignal("alice", "seed-phone", 0)}}, "catalog_items": []platform.Result{recoProduct("injected", "fake", "phone", "Sony", 1)}})
	if recoIDs(t, injected)[0] != "coffee" {
		t.Fatalf("client-supplied profile/catalog was trusted: %v", injected)
	}
	f.reset()
	guest := recoList(t, m, "", map[string]any{"user_id": "alice", "signals": map[string]any{"favorite": []platform.Result{recoSignal("alice", "seed-phone", 0)}}})
	if guest["personalized"] != false || f.count("engagement", "list") != 0 || f.count("order", "list") != 0 {
		t.Fatalf("guest fetched personal profile: %v", guest)
	}
}

func TestRecommendationTimeDecayAndSearchIntent(t *testing.T) {
	m, f := personalSetup(t)
	f.signals["alice"] = map[string][]platform.Result{"history": {recoSignal("alice", "seed-phone", 180*24*time.Hour), recoSignal("alice", "seed-coffee", 0)}}
	if got := recoIDs(t, recoList(t, m, "alice", nil))[0]; got != "coffee" {
		t.Fatalf("recent browsing did not outweigh old interest: %s", got)
	}
	f.signals["alice"] = map[string][]platform.Result{"search": {{"user_id": "alice", "query": "咖啡", "created_at": time.Now().UTC().Format(time.RFC3339)}}}
	r := recoList(t, m, "alice", nil)
	if recoIDs(t, r)[0] != "coffee" {
		t.Fatalf("recent search was ignored: %v", r)
	}
	encoded, _ := json.Marshal(r)
	if !strings.Contains(string(encoded), "recent_search") || !strings.Contains(string(encoded), "近期搜索") {
		t.Fatalf("search reason missing: %s", encoded)
	}
}

func TestRecommendationSimilarityUsesActualMetadataAndExcludesAnchor(t *testing.T) {
	m, f := personalSetup(t)
	r := recoList(t, m, "", map[string]any{"scene": "similar", "product_id": "seed-phone"})
	if recoIDs(t, r)[0] != "phone" || f.count("catalog", "batch_get") != 1 {
		t.Fatalf("anchor metadata not used: %v calls=%v", r, f.calls)
	}
	r = recoList(t, m, "", map[string]any{"scene": "similar", "product_id": "phone"})
	for _, id := range recoIDs(t, r) {
		if id == "phone" {
			t.Fatal("anchor recommended to itself")
		}
	}
	if _, err := m.Handle(context.Background(), platform.Command{Action: "list", Data: map[string]any{"scene": "similar", "product_id": "unknown"}}); status.Code(err) != codes.NotFound {
		t.Fatalf("missing anchor accepted: %v", err)
	}
}

func TestRecommendationPaidOrdersExcludeCanonicalProductButPendingAndRefundedDoNot(t *testing.T) {
	m, f := personalSetup(t)
	variant := recoProduct("phone--blue", "Blue phone", "手机数码", "Sony", 100000)
	variant["product_id"] = "phone"
	f.metadata["phone--blue"] = variant
	f.orders["alice"] = []platform.Result{{"user_id": "alice", "status": "COMPLETED", "items": []platform.Result{{"sku_id": "phone--blue"}}}, {"user_id": "alice", "status": "PENDING_PAYMENT", "items": []platform.Result{{"sku_id": "coffee"}}}, {"user_id": "alice", "status": "REFUNDED", "items": []platform.Result{{"sku_id": "sofa"}}}}
	r := recoList(t, m, "alice", nil)
	ids := recoIDs(t, r)
	if strings.Contains(strings.Join(ids, ","), "phone") || len(ids) != 2 {
		t.Fatalf("paid variant parent exclusion wrong: %v", ids)
	}
	if f.count("catalog", "batch_get") > 2 || f.count("inventory", "batch_get") != 1 || f.count("catalog", "get") != 0 || f.count("inventory", "get") != 0 {
		t.Fatalf("recommendation fanned out: %v", f.calls)
	}
}

func TestRecommendationOwnerIsolationAlsoForAdminsAndExplicitOrders(t *testing.T) {
	m, f := personalSetup(t)
	f.signals["admin"] = map[string][]platform.Result{"favorite": {recoSignal("admin", "seed-coffee", 0), recoSignal("victim", "seed-phone", 0)}}
	f.orders["admin"] = []platform.Result{{"user_id": "victim", "status": "COMPLETED", "items": []platform.Result{{"sku_id": "coffee"}}}}
	r, err := m.Handle(context.Background(), platform.Command{Action: "list", UserID: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if recoIDs(t, r)[0] != "coffee" || r["degraded"] != true {
		t.Fatalf("foreign signals affected admin's own profile: %v", r)
	}
	for _, call := range f.calls {
		if call.service == "engagement" || call.service == "order" {
			if call.command.UserID != "admin" || call.command.Role != "user" {
				t.Fatalf("broadened profile access: %#v", call)
			}
		}
	}
	f.contextOrder = platform.Result{"user_id": "victim", "status": "COMPLETED", "items": []platform.Result{{"sku_id": "coffee"}}}
	if _, err = m.Handle(context.Background(), platform.Command{Action: "list", UserID: "admin", Role: "admin", Data: map[string]any{"order_id": "victim-order"}}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another user's order used as profile: %v", err)
	}
}

func TestRecommendationBootstrapReusesTrustedReadsAndOnlyExportsSelectedInventory(t *testing.T) {
	m, f := personalSetup(t)
	stocks := []platform.Result{}
	for _, p := range f.base {
		stocks = append(stocks, platform.Result{"sku_id": p["id"], "available": 10})
	}
	data := map[string]any{"scene": "home", "limit": 2, "catalog_items": f.base, "inventory_items": stocks, "signals": map[string]any{"favorite": []platform.Result{recoSignal("alice", "coffee", 0)}}}
	r, err := m.Handle(context.Background(), platform.Command{Action: "rank_bootstrap", UserID: "alice", Role: "system", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if recoIDs(t, r)[0] != "coffee" || f.count("engagement", "list") != 0 || f.count("order", "list") != 0 || f.count("catalog", "list") != 0 || f.count("inventory", "batch_get") != 0 {
		t.Fatalf("bootstrap re-read trusted dependencies: %v calls=%v", r, f.calls)
	}
	if len(r["inventory_items"].([]map[string]any)) != 2 || len(r["stock_snapshot"].([]map[string]any)) != 3 {
		t.Fatalf("selected/all stock response mismatch: %v", r)
	}
	if _, err = m.Handle(context.Background(), platform.Command{Action: "rank_bootstrap", UserID: "alice", Role: "user", Data: data}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public user injected trusted profile: %v", err)
	}
	public := recoList(t, m, "", nil)
	if _, ok := public["stock_snapshot"]; ok {
		t.Fatal("public response exposed internal candidate snapshot")
	}
}

func TestRecommendationBoundedRecallDegradationAndDeterministicDiversity(t *testing.T) {
	m, f := personalSetup(t)
	// A broader cold-start pool should not fill every slot with one category.
	f.base = []platform.Result{}
	for i := 0; i < 6; i++ {
		p := recoProduct("same-"+string(rune('a'+i)), "相似手机", "手机数码", "Sony", 100000)
		f.base = append(f.base, p)
	}
	f.base = append(f.base, recoProduct("other", "咖啡", "食品生鲜", "LUMO", 10000))
	first := recoList(t, m, "", map[string]any{"limit": 4})
	second := recoList(t, m, "", map[string]any{"limit": 4})
	if !reflect.DeepEqual(recoIDs(t, first), recoIDs(t, second)) {
		t.Fatal("ranking changed without changed input")
	}
	if !strings.Contains(strings.Join(recoIDs(t, first), ","), "other") {
		t.Fatalf("cold start lacked diversity: %v", first)
	}
	f.signals["alice"] = map[string][]platform.Result{"favorite": {recoSignal("alice", "seed-phone", 0)}}
	f.fail["catalog/recommendation_candidates"] = status.Error(codes.DeadlineExceeded, "slow sparse recall")
	f.fail["order/list"] = status.Error(codes.Unavailable, "orders offline")
	r := recoList(t, m, "alice", nil)
	if r["degraded"] != true || len(recoIDs(t, r)) == 0 {
		t.Fatalf("auxiliary failure broke safe recommendations: %v", r)
	}
	f.fail["inventory/batch_get"] = status.Error(codes.Unavailable, "stock offline")
	if _, err := m.Handle(context.Background(), platform.Command{Action: "list"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("failed authoritative inventory invented availability: %v", err)
	}
}

func TestRecommendationValidationAndCancellation(t *testing.T) {
	m, _ := personalSetup(t)
	for _, data := range []map[string]any{{"scene": "unknown"}, {"scene": "similar"}, {"scene": "post_purchase"}, {"limit": 21}, {"limit": -1}, {"product_id": "bad\nID"}} {
		if _, err := m.Handle(context.Background(), platform.Command{Action: "list", Data: data}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid request accepted: %v -> %v", data, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Handle(ctx, platform.Command{Action: "list"}); status.Code(err) != codes.Canceled {
		t.Fatalf("cancellation was ignored: %v", err)
	}
}

func TestRecommendationTargetedRecallExpandsPoolAndPreservesPartialRecallFlag(t *testing.T) {
	m, f := personalSetup(t)
	f.signals["alice"] = map[string][]platform.Result{"favorite": {recoSignal("alice", "seed-phone", 0)}}
	f.recall = []platform.Result{recoProduct("recalled-phone", "无线耳机", "手机数码", "Sony", 100000)}
	f.recallDegraded = true
	r := recoList(t, m, "alice", nil)
	if recoIDs(t, r)[0] != "recalled-phone" || r["degraded"] != true {
		t.Fatalf("targeted result or partial-source flag lost: %v", r)
	}
	if f.count("catalog", "recommendation_candidates") != 1 || f.count("inventory", "batch_get") != 1 {
		t.Fatalf("unexpected recall/stock fanout: %#v", f.calls)
	}
}

func TestRecommendationCandidateAndStockPoolAreBounded(t *testing.T) {
	m, f := personalSetup(t)
	f.signals["alice"] = map[string][]platform.Result{"favorite": {recoSignal("alice", "seed-phone", 0)}}
	for i := 0; i < 600; i++ {
		id := fmt.Sprintf("recall-%03d", i)
		f.recall = append(f.recall, recoProduct(id, "耳机", "手机数码", "Sony", 100000))
	}
	r := recoList(t, m, "alice", map[string]any{"limit": 20})
	if len(recoIDs(t, r)) != 20 || f.count("inventory", "batch_get") != 1 {
		t.Fatalf("unbounded inventory requests: %v", r)
	}
	for _, call := range f.calls {
		if call.service == "inventory" {
			var data struct {
				IDs []string `json:"ids"`
			}
			_ = platform.Decode(call.command.Data, &data)
			if len(data.IDs) != 300 {
				t.Fatalf("candidate pool was not capped at 300: %d", len(data.IDs))
			}
		}
	}
}

func TestRecommendationPurchasedVariantBeyondSeedLimitStillExcludesParent(t *testing.T) {
	m, f := personalSetup(t)
	attributes := f.base[0]["attributes"].(map[string]any)
	attributes["variants"] = []map[string]any{{"sku_id": "phone--late"}}
	for i := 0; i < 100; i++ {
		f.orders["alice"] = append(f.orders["alice"], platform.Result{"user_id": "alice", "status": "COMPLETED", "items": []platform.Result{{"sku_id": fmt.Sprintf("old-%d-a", i)}, {"sku_id": fmt.Sprintf("old-%d-b", i)}}})
	}
	last := f.orders["alice"][99]
	last["items"] = append(last["items"].([]platform.Result), platform.Result{"sku_id": "phone--late"})
	r := recoList(t, m, "alice", nil)
	if strings.Contains(strings.Join(recoIDs(t, r), ","), "phone") {
		t.Fatalf("purchased option outside metadata seed leaked its parent: %v", r)
	}
	if f.count("catalog", "batch_get") != 1 || f.count("inventory", "batch_get") != 1 {
		t.Fatalf("purchase exclusion became unbounded: %#v", f.calls)
	}
}
