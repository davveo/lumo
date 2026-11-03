package tests_test

import (
	"context"
	"github.com/davveo/lumo/internal/bootstrap"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"sync"
	"testing"
	"time"
)

func TestMySQLProductExpansionMigrationsAndRedemptionConcurrency(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	router := expansionMySQLRouter{}
	dbs := map[string]*gorm.DB{}
	for _, name := range []string{"points", "coupon", "helpdesk", "analytics", "order", "fulfillment", "aftersale", "media", "catalog", "search", "inventory", "address", "member"} {
		ctx, db := adminMySQLScratch(t, "product_"+name)
		if e := platform.MigratePlatform(ctx, db); e != nil {
			t.Fatal(e)
		}
		m, e := bootstrap.New(name, platform.Dependencies{DB: db, RPC: router})
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			if e = m.Migrate(ctx); e != nil {
				t.Fatalf("%s: %v", name, e)
			}
		}
		if m.Seed != nil {
			if e = m.Seed(ctx); e != nil {
				t.Fatalf("seed %s: %v", name, e)
			}
		}
		router[name] = m
		dbs[name] = db
	}
	ctx := context.Background()
	call := func(name, action, id, key, user, role string, data map[string]any) (platform.Result, error) {
		return router.Call(
			ctx,
			name,
			platform.Command{Action: action, ID: id, IdempotencyKey: key, UserID: user, Role: role, Data: data},
		)
	}
	address, e := call(
		"address",
		"create",
		"",
		"",
		"demo",
		"user",
		map[string]any{
			"recipient": "测试",
			"phone":     "13800000000",
			"province":  "上海市",
			"city":      "上海市",
			"district":  "浦东新区",
			"address":   "测试路1号",
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	offer, e := call(
		"points",
		"offer_upsert",
		"",
		"",
		"admin",
		"admin",
		map[string]any{
			"name":         "测试礼品",
			"kind":         "GIFT",
			"reference_id": "sku-coffee",
			"cost":         200,
			"total_quota":  1,
			"max_per_user": 1,
			"active":       true,
			"starts_at":    time.Now().Add(-time.Hour).Format(time.RFC3339),
			"ends_at":      time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan platform.Result, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := call(
				"points",
				"redeem",
				platform.Text(offer, "id"),
				"same-click-request",
				"demo",
				"user",
				map[string]any{"version": platform.Int(offer, "version"), "address_id": platform.Text(address, "id")},
			)
			results <- out
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	id := ""
	for out := range results {
		if id != "" && platform.Text(out, "id") != id {
			t.Fatal("duplicate debit", out)
		}
		id = platform.Text(out, "id")
	}
	if e = router["points"].OnEvent(ctx, platform.Event{Type: "RedemptionRequested", AggregateID: id, UserID: "demo"}); e != nil {
		t.Fatal(e)
	}
	balance, e := call("points", "get", "", "", "demo", "user", nil)
	if e != nil || platform.Int(balance, "available") != 4800 {
		t.Fatal(balance, e)
	}
	_, e = call(
		"points",
		"redeem",
		platform.Text(offer, "id"),
		"second-click-request",
		"demo",
		"user",
		map[string]any{"version": platform.Int(offer, "version"), "address_id": platform.Text(address, "id")},
	)
	if status.Code(e) != codes.ResourceExhausted {
		t.Fatal(e)
	}
	if _, e = call("coupon", "reminder_update", "", "", "demo", "user", map[string]any{"enabled": true}); e != nil {
		t.Fatal(e)
	}
	if e = router["coupon"].Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = call("analytics", "track", "", "test-event", "", "", map[string]any{"type": "search", "query": "测试", "result_count": 0, "session_id": "session-1234567890", "visitor_id": "visitor-1234567890"}); e != nil {
		t.Fatal(e)
	}
	if _, e = call("analytics", "drilldown", "", "", "admin", "admin", map[string]any{"days": 7, "dimension": "query"}); e != nil {
		t.Fatal(e)
	}
	// Exercise the projected MySQL catalog search with every new filter.
	if _, e = call("catalog", "search_products", "", "", "system", "system", map[string]any{"query": "coffee", "min_price_cents": 1, "max_price_cents": 10000000, "attribute": "经典"}); e != nil {
		t.Fatal(e)
	}
}
