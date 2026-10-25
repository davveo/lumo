package tests

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

func marketingCampaign(name, segment, template string, priority int) map[string]any {
	return map[string]any{
		"name":       name,
		"status":     "PUBLISHED",
		"priority":   priority,
		"starts_at":  "",
		"ends_at":    "",
		"placements": []string{"home"},
		"audience": map[string]any{
			"segments":            []string{segment},
			"new_member_days":     7,
			"min_growth":          0,
			"max_growth":          0,
			"min_points":          0,
			"max_points":          0,
			"include_user_ids":    []string{},
			"exclude_user_ids":    []string{},
			"interest_categories": []string{},
		},
		"frequency": map[string]any{
			"max_per_user":         1,
			"max_per_day":          1,
			"cooldown_hours":       24,
			"once_per_session":     true,
			"close_cooldown_hours": 24,
			"stop_after_claim":     true,
		},
		"content": map[string]any{
			"title":              name,
			"subtitle":           "按人群展示的专属权益",
			"body":               "领券后按券面规则使用",
			"theme":              "red",
			"coupon_template_id": template,
			"button_text":        "立即领取",
			"action_type":        "claim_coupon",
			"action_target":      "#/coupons",
			"auto_close_seconds": 0,
			"delay_seconds":      0,
		},
	}
}
func marketingMap(t *testing.T, value any) platform.Result {
	t.Helper()
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result platform.Result
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func marketingPopup(t *testing.T, result platform.Result, campaign string) platform.Result {
	t.Helper()
	p := marketingMap(t, result["popup"])
	if p == nil || platform.Text(p, "campaign_id") != campaign {
		t.Fatalf("wrong targeted popup: expected=%s actual=%v", campaign, p)
	}
	for _, key := range []string{"audience", "frequency", "include_user_ids", "exclude_user_ids", "profile"} {
		if _, ok := p[key]; ok {
			t.Fatalf("private rule %s leaked to shopper", key)
		}
	}
	if platform.Text(p, "delivery_id") == "" {
		t.Fatal("missing opaque delivery")
	}
	return p
}
func marketingEvent(p platform.Result, event, view string) map[string]any {
	return map[string]any{"placement": "home", "delivery_id": p["delivery_id"], "view_id": view, "event": event}
}

// Exercises actual HTTP cookies, IAM validation and content -> coupon -> pricing
// RPC calls. Every database and business mutation belongs to newCluster's
// temporary isolated stack, never the developer's running storefront.
func TestHTTPMarketingPopupTargetingAndClaimLifecycle(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	newShop := func() *httpShop {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := *server.Client()
		client.Jar = jar
		return &httpShop{t: t, server: server, client: &client, coverage: map[string]int{}}
	}
	adminShop, guest, userShop, otherShop := newShop(), newShop(), newShop(), newShop()
	login := func(h *httpShop, name string) string {
		password := "Lumo@2026"
		if name == "admin" {
			password = "Admin@2026"
		}
		token := platform.Text(
			h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": password}),
			"token",
		)
		if token == "" {
			t.Fatal("login returned no token")
		}
		return token
	}
	admin := login(adminShop, "admin")
	// Start from explicit administrator configuration rather than depending on
	// the demo defaults' timing, audience order or future editorial changes.
	initial := adminShop.call(admin, "content", "popup_list", "", "", nil)
	for _, row := range mapItems(initial) {
		row["status"] = "PAUSED"
		adminShop.call(admin, "content", "popup_upsert", platform.Text(row, "id"), "", row)
	}
	adminShop.call(
		admin,
		"content",
		"popup_configure",
		"",
		"",
		map[string]any{"enabled": true, "max_per_session": 1, "min_gap_seconds": 0},
	)
	visitorCampaign := adminShop.call(
		admin,
		"content",
		"popup_upsert",
		"",
		"",
		marketingCampaign("首次访客见面礼", "new_guest", "everyday-10", 400),
	)
	visitorID := platform.Text(visitorCampaign, "id")
	guest.expect(401, "", "content", "popup_list", "", "", nil)
	userShop.expect(
		401,
		"",
		"storefront",
		"popup_claim",
		"",
		"",
		map[string]any{"delivery_id": "forged-delivery", "view_id": "view-forged"},
	)
	guest.expect(
		400,
		"",
		"storefront",
		"popup",
		"",
		"",
		map[string]any{"placement": "home", "profile": map[string]any{"level": "gold"}},
	)
	guest.expect(
		400,
		"",
		"storefront",
		"popup",
		"",
		"",
		map[string]any{"placement": "home", "visitor_id": strings.Repeat("a", 64)},
	)
	for _, entry := range [][2]string{
		{"content", "popup_decide"},
		{"content", "popup_event"},
		{"content", "popup_claim"},
		{"coupon", "templates_by_ids"},
	} {
		adminShop.expect(403, admin, entry[0], entry[1], "", "", nil)
	}
	guest.expect(404, "", "content", "get", visitorID, "", nil)
	first := marketingPopup(
		t,
		guest.call("", "storefront", "popup", "", "", map[string]any{"placement": "home"}),
		visitorID,
	)
	second := marketingPopup(
		t,
		guest.call("", "storefront", "popup", "", "", map[string]any{"placement": "home"}),
		visitorID,
	)
	if platform.Text(first, "delivery_id") == platform.Text(second, "delivery_id") {
		t.Fatal("expected independent pending delivery tokens")
	}
	impression := marketingEvent(first, "impression", "guest-view")
	if guest.call("", "storefront", "popup_event", "", "", impression)["allowed"] != true ||
		guest.call("", "storefront", "popup_event", "", "", impression)["allowed"] != true {
		t.Fatal("valid impression/retry rejected")
	}
	if guest.call(
		"",
		"storefront",
		"popup_event",
		"",
		"",
		marketingEvent(second, "impression", "guest-other-tab"),
	)["allowed"] != false {
		t.Fatal("second tab bypassed global session cap")
	}
	guest.call("", "storefront", "popup_event", "", "", marketingEvent(first, "dismiss", "guest-view"))
	guest.call("", "storefront", "popup_event", "", "", marketingEvent(first, "auto_close", "guest-view"))
	if result := guest.call("", "storefront", "popup", "", "", nil); result["popup"] != nil {
		t.Fatal("shown/closed visitor welcome repeated")
	}
	if result := guest.call(
		"",
		"storefront",
		"popup",
		"",
		"",
		map[string]any{"placement": "product"},
	); result["popup"] != nil {
		t.Fatal("home-only popup appeared on product")
	}
	freshGuest := newShop()
	marketingPopup(t, freshGuest.call("", "storefront", "popup", "", "", nil), visitorID)
	// Keep the visitor but start a different browser session: it is a returning
	// visitor, so a new_guest campaign must no longer qualify.
	origin, _ := url.Parse(server.URL)
	cookies := guest.client.Jar.Cookies(origin)
	for _, cookie := range cookies {
		if cookie.Name == "lumo_popup_session" {
			cookie.Value = strings.Repeat("b", 64)
			guest.client.Jar.SetCookies(origin, []*http.Cookie{cookie})
		}
	}
	if result := guest.call("", "storefront", "popup", "", "", nil); result["popup"] != nil {
		t.Fatal("returning visitor was treated as first visit")
	}
	// IAM account age, not the lazily created member profile, selects the old
	// member campaign. Modify only this test's isolated IAM account fixture.
	oldRegistered := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if err := c.dbs["iam"].Table("iam_accounts").Where("id = ?", "demo").Update(
		"created_at",
		oldRegistered,
	).Error; err != nil {
		t.Fatal(err)
	}
	oldCampaign := adminShop.call(
		admin,
		"content",
		"popup_upsert",
		"",
		"",
		marketingCampaign("老会员回归礼", "returning_member", "weekend-25", 500),
	)
	oldID := platform.Text(oldCampaign, "id")
	newCampaign := adminShop.call(
		admin,
		"content",
		"popup_upsert",
		"",
		"",
		marketingCampaign("新会员专属礼", "new_member", "welcome-100", 600),
	)
	newID := platform.Text(newCampaign, "id")
	user := login(userShop, "demo")
	member := userShop.call(user, "member", "get", "", "", nil)
	created, err := time.Parse(time.RFC3339, platform.Text(member, "created_at"))
	if err != nil || created.Before(time.Now().UTC().Add(-time.Hour)) {
		t.Fatalf("member profile fixture must be newly created: %v %v", member, err)
	}
	oldDelivery := marketingPopup(t, userShop.call(user, "storefront", "popup", "", "", nil), oldID)
	// Old pending payload is invalidated when the operator pauses its campaign.
	oldCampaign["status"] = "PAUSED"
	adminShop.call(admin, "content", "popup_upsert", oldID, "", oldCampaign)
	if userShop.call(
		user,
		"storefront",
		"popup_event",
		"",
		"",
		marketingEvent(oldDelivery, "impression", "old-view"),
	)["allowed"] != false {
		t.Fatal("pause did not invalidate pending delivery")
	}
	if result := userShop.call(user, "storefront", "popup", "", "", nil); result["popup"] != nil {
		t.Fatal("old account received new-member campaign after lazy member creation")
	}
	if err := c.dbs["iam"].Table("iam_accounts").Where("id = ?", "demo").Update(
		"created_at",
		time.Now().UTC().Add(-24*time.Hour),
	).Error; err != nil {
		t.Fatal(err)
	}
	pending := marketingPopup(t, userShop.call(user, "storefront", "popup", "", "", nil), newID)
	content := marketingMap(t, newCampaign["content"])
	content["title"] = "更新后的新会员专属礼"
	newCampaign["content"] = content
	updated := adminShop.call(admin, "content", "popup_upsert", newID, "", newCampaign)
	if userShop.call(
		user,
		"storefront",
		"popup_event",
		"",
		"",
		marketingEvent(pending, "impression", "stale-member-view"),
	)["allowed"] != false {
		t.Fatal("updated content did not invalidate pending payload")
	}
	adminShop.expect(409, admin, "content", "popup_upsert", newID, "", newCampaign)
	delivery := marketingPopup(t, userShop.call(user, "storefront", "popup", "", "", nil), newID)
	if platform.Text(marketingMap(t, delivery["content"]), "title") != "更新后的新会员专属礼" {
		t.Fatal("shopper did not receive published content")
	}
	if userShop.call(
		user,
		"storefront",
		"popup_event",
		"",
		"",
		marketingEvent(delivery, "impression", "member-view"),
	)["allowed"] != true {
		t.Fatal("member impression denied")
	}
	// A second real IAM account cannot reuse another member's delivery even
	// when it knows the opaque token and view ID.
	var account struct{ PasswordHash string }
	if err := c.dbs["iam"].Table("iam_accounts").Select("password_hash").Where("id = ?", "demo").Take(
		&account,
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := c.dbs["iam"].Table("iam_accounts").Create(
		map[string]any{
			"id":            "popup-other",
			"username":      "popup-other",
			"password_hash": account.PasswordHash,
			"role":          "user",
			"active":        true,
			"created_at":    time.Now().UTC(),
			"updated_at":    time.Now().UTC(),
		},
	).Error; err != nil {
		t.Fatal(err)
	}
	other := login(otherShop, "popup-other")
	if otherShop.call(
		other,
		"storefront",
		"popup_event",
		"",
		"",
		marketingEvent(delivery, "click", "member-view"),
	)["allowed"] != false {
		t.Fatal("delivery crossed authenticated subjects")
	}
	claimData := map[string]any{"placement": "home", "delivery_id": delivery["delivery_id"], "view_id": "member-view"}
	otherShop.expect(409, other, "storefront", "popup_claim", "", "", claimData)
	var before struct{ Remaining int64 }
	if err := c.dbs["coupon"].Table("templates").Select("remaining").Where("id = ?", "welcome-100").Take(
		&before,
	).Error; err != nil {
		t.Fatal(err)
	}
	issued := userShop.call(user, "storefront", "popup_claim", "", "", claimData)
	coupon := marketingMap(t, issued["coupon"])
	couponID := platform.Text(coupon, "id")
	if couponID == "" || platform.Text(coupon, "template_id") != "welcome-100" ||
		platform.Int(coupon, "amount_cents") != 10000 {
		t.Fatalf("popup did not issue configured real coupon: %v", issued)
	}
	repeated := userShop.call(user, "storefront", "popup_claim", "", "", claimData)
	if platform.Text(marketingMap(t, repeated["coupon"]), "id") != couponID || repeated["already_claimed"] != true {
		t.Fatal("claim retry was not idempotent")
	}
	var after struct{ Remaining int64 }
	if err := c.dbs["coupon"].Table("templates").Select("remaining").Where("id = ?", "welcome-100").Take(
		&after,
	).Error; err != nil {
		t.Fatal(err)
	}
	if before.Remaining-after.Remaining != 1 {
		t.Fatalf("claim retry consumed multiple quota: before=%d after=%d", before.Remaining, after.Remaining)
	}
	wallet := userShop.call(user, "coupon", "list", "", "", nil)
	matches := 0
	for _, row := range mapItems(wallet) {
		if platform.Text(row, "template_id") == "welcome-100" {
			matches++
			if platform.Text(row, "id") != couponID || platform.Text(row, "user_id") != "demo" {
				t.Fatal("coupon wallet ownership mismatch")
			}
		}
	}
	if matches != 1 {
		t.Fatalf("expected exactly one issued wallet coupon, got %d", matches)
	}
	quote := userShop.call(
		user,
		"checkout",
		"quote",
		"",
		"",
		map[string]any{"items": []any{map[string]any{"sku_id": "iphone", "quantity": 1}}, "coupon_id": couponID},
	)
	if platform.Int(quote, "coupon_discount_cents") != 10000 || platform.Text(quote, "coupon_id") != couponID ||
		platform.Int(quote, "payable_cents") <= 0 {
		t.Fatalf("issued popup coupon did not enter actual pricing: %v", quote)
	}
	if result := userShop.call(user, "storefront", "popup", "", "", nil); result["popup"] != nil {
		t.Fatal("claimed campaign repeated")
	}
	// Administration reports real impressions and one successful issuance. View
	// retries, duplicate close events, and coupon retries do not inflate totals.
	summary := adminShop.call(admin, "content", "popup_list", "", "", nil)
	for _, row := range mapItems(summary) {
		stats := marketingMap(t, row["stats"])
		switch platform.Text(row, "id") {
		case visitorID:
			if platform.Int(stats, "impressions") != 1 || platform.Int(stats, "dismissals") != 1 ||
				platform.Int(stats, "claims") != 0 {
				t.Fatalf("guest stats inflated: %v", stats)
			}
		case newID:
			if platform.Int(stats, "impressions") != 1 || platform.Int(stats, "claims") != 1 ||
				platform.Int(row, "version") != platform.Int(updated, "version") {
				t.Fatalf("member stats/version inflated: %v", row)
			}
		}
	}
}
