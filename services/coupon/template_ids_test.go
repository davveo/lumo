package coupon_test

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func TestInternalTemplateLookupIsBoundedAndKeepsAuthoritativeEligibility(t *testing.T) {
	caller := &centerCaller{user: "buyer", points: 4999, growth: 999}
	db, module := setup(t, caller)
	rows := make([]map[string]any, 0, 150)
	for index := 0; index < 150; index++ {
		rows = append(rows, map[string]any{"id": fmt.Sprintf("a-lookup-%03d", index), "name": "其他券", "amount": 100, "minimum": 1000, "valid_days": 7, "remaining": 10, "active": true, "expires_at": time.Now().AddDate(1, 0, 0)})
	}
	if err := db.Table("templates").CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	// The selected coupon is deliberately outside the ordinary first 100 page.
	first := must(t, module, "templates", "", "", "", nil)
	for _, row := range first["items"].([]platform.Result) {
		if row["id"] == "welcome-100" {
			t.Fatal("fixture must require direct lookup")
		}
	}
	var queries atomic.Int64
	if err := db.Callback().Query().Before("gorm:query").Register("test:popup_lookup_count", func(*gorm.DB) { queries.Add(1) }); err != nil {
		t.Fatal(err)
	}
	result := must(t, module, "templates_by_ids", "", "buyer", "system", map[string]any{"ids": []any{"welcome-100", "points-30", "member-80", "welcome-100", "missing"}, "points": 999999, "growth": 999999})
	items := map[string]platform.Result{}
	for _, item := range result["items"].([]platform.Result) {
		items[platform.Text(item, "id")] = item
	}
	if len(items) != 3 || platform.Int(result, "total") != 3 || queries.Load() != 2 {
		t.Fatalf("lookup scanned pages or repeated queries: items=%d queries=%d", len(items), queries.Load())
	}
	expectCenterState(t, items, "welcome-100", "available")
	expectCenterState(t, items, "points-30", "conditions_unmet")
	expectCenterState(t, items, "member-80", "conditions_unmet")
	if caller.calls["points"] != 1 || caller.calls["member"] != 1 {
		t.Fatal("eligibility repeated per coupon", caller.calls)
	}
	caller.points = 5000
	caller.growth = 1000
	claim := must(t, module, "claim", "", "buyer", "user", map[string]any{"template_id": "points-30"})
	result = must(t, module, "templates_by_ids", "", "buyer", "system", map[string]any{"ids": []string{"points-30", "upcoming-50"}})
	items = map[string]platform.Result{}
	for _, item := range result["items"].([]platform.Result) {
		items[platform.Text(item, "id")] = item
	}
	expectCenterState(t, items, "points-30", "claimed")
	expectCenterState(t, items, "upcoming-50", "upcoming")
	if items["points-30"]["coupon_id"] != claim["id"] {
		t.Fatal("claimed lookup lost issued coupon id")
	}
	if err := db.Table("templates").Where("id = ?", "welcome-100").Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	result = must(t, module, "templates_by_ids", "", "", "system", map[string]any{"ids": []string{"welcome-100", "everyday-10"}})
	items = map[string]platform.Result{}
	for _, item := range result["items"].([]platform.Result) {
		items[platform.Text(item, "id")] = item
	}
	expectCenterState(t, items, "welcome-100", "unavailable")
	expectCenterState(t, items, "everyday-10", "login_required")
}
func TestInternalTemplateLookupRejectsBrowserPrincipalsAndMalformedIDs(t *testing.T) {
	_, module := setup(t, nil)
	for _, role := range []string{"", "user", "admin"} {
		if _, err := invoke(module, "templates_by_ids", "", "buyer", role, map[string]any{"ids": []string{"welcome-100"}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("non-service principal accepted: %s %v", role, err)
		}
	}
	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = "welcome-100"
	}
	for _, ids := range []any{nil, []any{}, "welcome-100", []any{1}, []string{""}, []string{" welcome-100"}, []string{"bad\x00id"}, []string{strings.Repeat("a", 121)}, tooMany} {
		if _, err := invoke(module, "templates_by_ids", "", "", "system", map[string]any{"ids": ids}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid IDs accepted: %#v %v", ids, err)
		}
	}
}
