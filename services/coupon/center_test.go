package coupon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type centerCaller struct {
	mu             sync.Mutex
	points, growth int64
	fail           bool
	calls          map[string]int
	user           string
}

func (c *centerCaller) Call(ctx context.Context, name string, cmd platform.Command) (platform.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("missing RPC deadline")
	}
	if cmd.UserID != c.user || cmd.Action != "get" || cmd.ID != "" || len(cmd.Data) != 0 {
		return nil, errors.New("unexpected eligibility identity or command")
	}
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[name]++
	if c.fail {
		return nil, errors.New("service unavailable")
	}
	switch name {
	case "points":
		return platform.Result{"available": c.points}, nil
	case "member":
		return platform.Result{"growth": c.growth}, nil
	}
	return nil, errors.New("unexpected service")
}

func centerItems(
	t *testing.T,
	m platform.Module,
	user string,
	data map[string]any,
) (platform.Result, map[string]platform.Result) {
	t.Helper()
	out := must(t, m, "templates", "", user, "user", data)
	items := map[string]platform.Result{}
	for _, item := range out["items"].([]platform.Result) {
		items[platform.Text(item, "id")] = item
	}
	return out, items
}
func expectCenterState(t *testing.T, items map[string]platform.Result, id, state string) {
	t.Helper()
	item := items[id]
	if item == nil || item["claim_status"] != state || item["claimable"] != (state == "available") ||
		platform.Text(item, "claim_reason") == "" {
		t.Fatalf("%s expected %s: %v", id, state, item)
	}
}

func TestTemplateSnapshotBatchesLargeCenterAndEligibilityReads(t *testing.T) {
	caller := &centerCaller{points: 5000, growth: 1000, user: "demo"}
	db, module := setup(t, caller)
	now := time.Now()
	rows := make([]map[string]any, 0, 2001)
	for index := 0; index < 2001; index++ {
		rows = append(
			rows,
			map[string]any{
				"id":         fmt.Sprintf("snapshot-%04d", index),
				"name":       "snapshot coupon",
				"amount":     100,
				"minimum":    1000,
				"valid_days": 7,
				"remaining":  10,
				"active":     true,
				"expires_at": now.AddDate(1, 0, 0),
				"created_at": now,
			},
		)
	}
	if err := db.Table("templates").CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	result := must(t, module, "templates_snapshot", "", "demo", "user", map[string]any{"page": 1, "page_size": 5000})
	if len(result["items"].([]platform.Result)) != 2007 || platform.Int(result, "total") != 2007 ||
		result["has_more"] != false {
		t.Fatalf("snapshot truncated the complete center: %v", result["total"])
	}
	if caller.calls["points"] != 1 || caller.calls["member"] != 1 {
		t.Fatalf("eligibility reads grew with template count: %v", caller.calls)
	}
	for _, tc := range []struct {
		action string
		size   int64
	}{{"templates", 101}, {"templates_snapshot", 5001}} {
		if _, err := invoke(module, tc.action, "", "demo", "user", map[string]any{"page_size": tc.size}); status.Code(
			err,
		) != codes.InvalidArgument {
			t.Fatalf("%s accepted page size %d: %v", tc.action, tc.size, err)
		}
	}
}
func TestCenterAllStatesAndPagination(t *testing.T) {
	db, m := setup(t, nil)
	// Active ended/exhausted templates remain discoverable, disabled templates do not.
	if err := db.Table("templates").Where("id = ?", "welcome-100").Update(
		"expires_at",
		time.Now().Add(-time.Minute),
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("templates").Where("id = ?", "weekend-25").Update("remaining", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("templates").Where("id = ?", "member-80").Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	out, items := centerItems(t, m, "", nil)
	if platform.Int(out, "total") != 5 || out["has_more"] != false || out["server_time"] == nil ||
		items["member-80"] != nil {
		t.Fatal(out)
	}
	expectCenterState(t, items, "welcome-100", "ended")
	expectCenterState(t, items, "weekend-25", "exhausted")
	expectCenterState(t, items, "upcoming-50", "upcoming")
	expectCenterState(t, items, "everyday-10", "login_required")
	expectCenterState(t, items, "points-30", "login_required")
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		out, items := centerItems(t, m, "", map[string]any{"page": page, "page_size": 2})
		if platform.Int(out, "page") != int64(page) || platform.Int(out, "page_size") != 2 ||
			out["has_more"] != (page < 3) {
			t.Fatal(out)
		}
		for id := range items {
			if seen[id] {
				t.Fatalf("duplicate page item %s", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 5 {
		t.Fatal(seen)
	}
	for _, data := range []map[string]any{{"page": -1}, {"page_size": 101}, {"page_size": -1}, {"page": 1000001}} {
		if _, err := invoke(m, "templates", "", "", "", data); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid pagination accepted: %v %v", data, err)
		}
	}
}
func TestCenterTrustedConditionsAndClaimIdempotency(t *testing.T) {
	caller := &centerCaller{user: "buyer", points: 4999, growth: 999}
	db, m := setup(t, caller)
	_, items := centerItems(t, m, "buyer", map[string]any{"available": 999999, "growth": 999999})
	expectCenterState(t, items, "everyday-10", "available")
	expectCenterState(t, items, "points-30", "conditions_unmet")
	expectCenterState(t, items, "member-80", "conditions_unmet")
	// Copy a second conditional template to assert one read per service per listing.
	if err := db.Exec(
		"INSERT INTO templates(id,name,amount,minimum,valid_days,remaining,active,min_points,min_growth,expires_at) SELECT 'points-copy',name,amount,minimum,valid_days,remaining,active,min_points,min_growth,expires_at FROM templates WHERE id='points-30'",
	).Error; err != nil {
		t.Fatal(err)
	}
	caller.calls = map[string]int{}
	centerItems(t, m, "buyer", nil)
	if caller.calls["points"] != 1 || caller.calls["member"] != 1 {
		t.Fatal(caller.calls)
	}
	for _, id := range []string{"points-30", "member-80"} {
		if _, err := invoke(
			m,
			"claim",
			"",
			"buyer",
			"user",
			map[string]any{"template_id": id, "available": 999999, "growth": 999999, "min_points": 0, "min_growth": 0},
		); status.Code(
			err,
		) != codes.FailedPrecondition {
			t.Fatalf("forged conditions allowed %s: %v", id, err)
		}
	}
	caller.points = 5000
	caller.growth = 1000
	_, items = centerItems(t, m, "buyer", nil)
	expectCenterState(t, items, "points-30", "available")
	expectCenterState(t, items, "member-80", "available")
	one := must(t, m, "claim", "", "buyer", "user", map[string]any{"template_id": "points-30"})
	if platform.Int(one, "amount_cents") != 3000 || platform.Int(one, "min_cents") != 30000 ||
		platform.Text(one, "user_id") != "buyer" {
		t.Fatal(one)
	}
	// Retrying an existing claim remains successful even when every current rule fails.
	if err := db.Table("templates").Where("id = ?", "points-30").Updates(
		map[string]any{"active": false, "remaining": 0, "expires_at": time.Now().Add(-time.Hour), "min_points": 100000},
	).Error; err != nil {
		t.Fatal(err)
	}
	caller.fail = true
	two := must(t, m, "claim", "", "buyer", "user", map[string]any{"template_id": "points-30"})
	if one["id"] != two["id"] || two["already_claimed"] != true {
		t.Fatal(one, two)
	}
	// No points update is issued by the coupon service.
	if caller.points != 5000 {
		t.Fatal("claim spent points")
	}
}
func TestCenterUnavailableConditionsDoNotBlockOrdinaryCoupons(t *testing.T) {
	caller := &centerCaller{user: "buyer", fail: true}
	_, m := setup(t, caller)
	_, items := centerItems(t, m, "buyer", nil)
	expectCenterState(t, items, "points-30", "unavailable")
	expectCenterState(t, items, "member-80", "unavailable")
	expectCenterState(t, items, "everyday-10", "available")
	must(t, m, "claim", "", "buyer", "user", map[string]any{"template_id": "everyday-10"})
	if _, err := invoke(m, "claim", "", "buyer", "user", map[string]any{"template_id": "points-30"}); status.Code(
		err,
	) != codes.Unavailable {
		t.Fatalf("unverifiable conditional claim accepted: %v", err)
	}
	_, withoutRPC := setup(t, nil)
	_, items = centerItems(t, withoutRPC, "buyer", nil)
	expectCenterState(t, items, "points-30", "unavailable")
	must(t, withoutRPC, "claim", "", "buyer", "user", map[string]any{"template_id": "welcome-100"})
}
func TestCenterTimeWindowsQuotaAndClaimedPriority(t *testing.T) {
	db, m := setup(t, nil)
	for _, test := range []struct {
		id      string
		updates map[string]any
		code    codes.Code
	}{
		{"upcoming-50", nil, codes.FailedPrecondition},
		{"everyday-10", map[string]any{"expires_at": time.Now().Add(-time.Minute)}, codes.FailedPrecondition},
		{"weekend-25", map[string]any{"remaining": 0}, codes.ResourceExhausted},
		{"welcome-100", map[string]any{"active": false}, codes.FailedPrecondition},
	} {
		if test.updates != nil {
			if err := db.Table("templates").Where("id = ?", test.id).Updates(test.updates).Error; err != nil {
				t.Fatal(err)
			}
		}
		if _, err := invoke(m, "claim", "", "buyer", "user", map[string]any{"template_id": test.id}); status.Code(
			err,
		) != test.code {
			t.Fatalf("%s: %v", test.id, err)
		}
	}
	if err := db.Table("templates").Where("id = ?", "everyday-10").Updates(
		map[string]any{"expires_at": time.Now().Add(time.Hour)},
	).Error; err != nil {
		t.Fatal(err)
	}
	one := must(t, m, "claim", "", "buyer", "user", map[string]any{"template_id": "everyday-10"})
	if err := db.Table("templates").Where("id = ?", "everyday-10").Updates(
		map[string]any{"expires_at": time.Now().Add(-time.Hour), "remaining": 0},
	).Error; err != nil {
		t.Fatal(err)
	}
	_, items := centerItems(t, m, "buyer", nil)
	expectCenterState(t, items, "everyday-10", "claimed")
	if items["everyday-10"]["coupon_id"] != one["id"] || items["everyday-10"]["claimed"] != true {
		t.Fatal(items)
	}
	_, other := centerItems(t, m, "other", nil)
	expectCenterState(t, other, "everyday-10", "ended")
}
func TestCenterSeedPreservesOperatorConfiguration(t *testing.T) {
	db, m := setup(t, nil)
	if err := db.Table("templates").Where("id = ?", "points-30").Updates(
		map[string]any{"name": "运营配置", "remaining": 17, "min_points": 123},
	).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.Seed(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var configured struct {
		Name                 string
		Remaining, MinPoints int64
	}
	if err := db.Table("templates").Where("id = ?", "points-30").Take(&configured).Error; err != nil {
		t.Fatal(err)
	}
	if configured.Name != "运营配置" || configured.Remaining != 17 || configured.MinPoints != 123 {
		t.Fatal(configured)
	}
	_, items := centerItems(t, m, "", nil)
	if len(items) != 6 || !strings.Contains(platform.Text(items["points-30"], "name"), "运营") {
		t.Fatal(items)
	}
}

func TestCenterConcurrentRepeatClaimsReturnOneCoupon(t *testing.T) {
	db, m := setup(t, nil)
	results := make(chan platform.Result, 12)
	errs := make(chan error, 12)
	for range 12 {
		go func() {
			v, err := invoke(m, "claim", "", "same-buyer", "user", map[string]any{"template_id": "everyday-10"})
			results <- v
			errs <- err
		}()
	}
	id := ""
	for range 12 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		v := <-results
		if id == "" {
			id = platform.Text(v, "id")
		}
		if platform.Text(v, "id") != id {
			t.Fatal("repeat claims created different coupons", v, id)
		}
	}
	var count int64
	if err := db.Table("claims").Where("user_id = ? AND template_id = ?", "same-buyer", "everyday-10").Count(
		&count,
	).Error; err != nil ||
		count != 1 {
		t.Fatal(count, err)
	}
	var remaining int64
	if err := db.Table("templates").Select("remaining").Where("id = ?", "everyday-10").Scan(
		&remaining,
	).Error; err != nil ||
		remaining != 1999 {
		t.Fatal(remaining, err)
	}
}

func TestCenterBothConditionsAndInvalidPolicy(t *testing.T) {
	caller := &centerCaller{user: "buyer", points: 5000, growth: 999}
	db, m := setup(t, caller)
	if err := db.Table("templates").Where("id = ?", "points-30").Update("min_growth", 1000).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(m, "claim", "", "buyer", "user", map[string]any{"template_id": "points-30"}); status.Code(
		err,
	) != codes.FailedPrecondition {
		t.Fatal(err)
	}
	caller.growth = 1000
	must(t, m, "claim", "", "buyer", "user", map[string]any{"template_id": "points-30"})
	for _, updates := range []map[string]any{
		{"min_points": -1},
		{"min_points": 0, "valid_days": 0},
		{"valid_days": 7, "starts_at": time.Now().Add(48 * time.Hour), "expires_at": time.Now().Add(time.Hour)},
	} {
		if err := db.Table("templates").Where("id = ?", "everyday-10").Updates(updates).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := invoke(m, "claim", "", "buyer", "user", map[string]any{"template_id": "everyday-10"}); status.Code(
			err,
		) != codes.FailedPrecondition {
			t.Fatal(err)
		}
	}
}

func TestCouponWalletNamesDoNotDependOnPublicTemplatePagesOrStatus(t *testing.T) {
	db, m := setup(t, nil)
	// Put the owned template beyond the first public page before claiming it.
	for i := 0; i < 110; i++ {
		if err := db.Table("templates").Create(
			map[string]any{
				"id":         fmt.Sprintf("a-wallet-page-%03d", i),
				"name":       "其他券",
				"amount":     100,
				"minimum":    1000,
				"valid_days": 7,
				"remaining":  1,
				"active":     true,
				"expires_at": time.Now().Add(time.Hour),
			},
		).Error; err != nil {
			t.Fatal(err)
		}
	}
	one := must(t, m, "claim", "", "demo", "user", map[string]any{"template_id": "weekend-25"})
	if err := db.Table("templates").Where("id = ?", "weekend-25").Updates(
		map[string]any{"active": false, "name": "已停用的限时生活礼券"},
	).Error; err != nil {
		t.Fatal(err)
	}
	_, templates := centerItems(t, m, "demo", map[string]any{"page": 1, "page_size": 100})
	if templates["weekend-25"] != nil || len(templates) != 100 {
		t.Fatal("fixture must not appear on the first public template page")
	}
	out := must(t, m, "list", "", "demo", "user", nil)
	raw, err := json.Marshal(out["items"])
	if err != nil {
		t.Fatal(err)
	}
	var rows []platform.Result
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		switch row["id"] {
		case one["id"]:
			if row["template_name"] != "已停用的限时生活礼券" || row["template_id"] != "weekend-25" ||
				row["status"] != "AVAILABLE" {
				t.Fatal(row)
			}
		case "coupon-demo-100":
			if row["template_name"] != "品质生活优惠券" {
				t.Fatal("legacy coupon without a template must have a fallback name", row)
			}
		default:
			t.Fatal("unexpected wallet row", row)
		}
	}
	other := must(t, m, "list", "", "other", "user", nil)
	raw, err = json.Marshal(other["items"])
	if err != nil || string(raw) != "[]" {
		t.Fatal("wallet join must preserve coupon ownership and an empty array", string(raw), err)
	}
}
