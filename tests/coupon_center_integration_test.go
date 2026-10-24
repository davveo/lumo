package tests

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

// The center is public, but its personalized eligibility and every claim enter
// the same production gateway and real service RPCs used by checkout.
func TestHTTPCouponCenterEligibilityThroughPaymentAndRefund(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	now := time.Now().UTC()
	seed := func(id string, changes map[string]any) {
		t.Helper()
		row := map[string]any{
			"id":         id,
			"name":       id,
			"amount":     int64(1000),
			"minimum":    int64(10000),
			"valid_days": int64(30),
			"remaining":  int64(5),
			"active":     true,
			"expires_at": now.Add(24 * time.Hour),
			"created_at": now,
		}
		for key, value := range changes {
			row[key] = value
		}
		if err := c.dbs["coupon"].Table("templates").Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	seed("integration-open", nil)
	seed("integration-upcoming", map[string]any{"starts_at": now.Add(time.Hour)})
	seed("integration-ended", map[string]any{"expires_at": now.Add(-time.Hour)})
	seed("integration-exhausted", map[string]any{"remaining": int64(0)})
	seed("integration-points", map[string]any{"min_points": int64(5001)})
	seed("integration-growth", map[string]any{"min_growth": int64(1000)})
	seed("integration-disabled", map[string]any{"active": false})

	allPages := func(token string) map[string]map[string]any {
		t.Helper()
		rows := map[string]map[string]any{}
		var total int64
		for page := int64(1); page <= 100; page++ {
			result := h.call(
				token,
				"coupon",
				"templates",
				"",
				"",
				map[string]any{
					"page":      page,
					"page_size": int64(2),
					"user_id":   "demo",
					"available": 999999,
					"growth":    999999,
				},
			)
			if platform.Int(result, "page") != page || platform.Int(result, "page_size") != 2 ||
				platform.Text(result, "server_time") == "" {
				t.Fatalf("center pagination/server clock missing: %v", result)
			}
			if page == 1 {
				total = platform.Int(result, "total")
			} else if platform.Int(result, "total") != total {
				t.Fatalf("center total changed while listing fixed fixtures: %v", result)
			}
			items := mapItems(result)
			if len(items) > 2 {
				t.Fatalf("center ignored page size: %v", result)
			}
			for _, item := range items {
				id := platform.Text(item, "id")
				if _, exists := rows[id]; exists {
					t.Fatalf("coupon %s was duplicated across pages", id)
				}
				rows[id] = item
			}
			if result["has_more"] != true {
				if int64(len(rows)) != total {
					t.Fatalf("center silently truncated templates: total=%d got=%d", total, len(rows))
				}
				return rows
			}
		}
		t.Fatal("center pagination did not finish")
		return nil
	}
	assertStatus := func(rows map[string]map[string]any, id, want string) {
		t.Helper()
		row, ok := rows[id]
		if !ok || platform.Text(row, "claim_status") != want || platform.Text(row, "claim_reason") == "" {
			t.Fatalf("coupon %s state want %s, got %v", id, want, row)
		}
		if row["claimable"] != (want == "available") {
			t.Fatalf("coupon %s claimable disagrees with eligibility: %v", id, row)
		}
	}
	guest := allPages("")
	for id, state := range map[string]string{
		"integration-open":      "login_required",
		"integration-upcoming":  "upcoming",
		"integration-ended":     "ended",
		"integration-exhausted": "exhausted",
		"integration-points":    "login_required",
		"integration-growth":    "login_required",
	} {
		assertStatus(guest, id, state)
	}
	if _, ok := guest["integration-disabled"]; ok {
		t.Fatal("disabled unpublished coupon appeared in public center")
	}
	h.expect(401, "", "coupon", "claim", "", "", map[string]any{"template_id": "integration-open", "user_id": "demo"})
	h.expect(401, "invalid-token", "coupon", "templates", "", "", nil)
	token := platform.Text(
		h.call("", "iam", "login", "", "", map[string]any{"username": "demo", "password": "Lumo@2026"}),
		"token",
	)
	member := allPages(token)
	assertStatus(member, "integration-open", "available")
	assertStatus(member, "integration-points", "conditions_unmet")
	assertStatus(member, "integration-growth", "conditions_unmet")
	for _, id := range []string{
		"integration-points",
		"integration-growth",
		"integration-upcoming",
		"integration-ended",
	} {
		h.expect(
			409,
			token,
			"coupon",
			"claim",
			"",
			"",
			map[string]any{
				"template_id": id,
				"user_id":     "admin",
				"available":   999999,
				"points":      999999,
				"growth":      999999,
				"min_points":  0,
				"min_growth":  0,
				"starts_at":   now.Add(-time.Hour),
			},
		)
	}
	h.expect(429, token, "coupon", "claim", "", "", map[string]any{"template_id": "integration-exhausted"})

	// Daily check-in changes the real points account; eligibility cannot be forged in
	// a claim payload, and the threshold is a condition rather than a payment.
	beforePoints := h.call(token, "points", "get", "", "", nil)
	h.call(token, "points", "checkin", "", "", nil)
	qualifiedPoints := h.call(token, "points", "get", "", "", nil)
	if platform.Int(qualifiedPoints, "available") <= platform.Int(beforePoints, "available") {
		t.Fatal("real check-in did not raise the member's available points")
	}
	assertStatus(allPages(token), "integration-points", "available")
	claim := h.call(token, "coupon", "claim", "", "", map[string]any{"template_id": "integration-points"})
	couponID := platform.Text(claim, "id")
	if couponID == "" || platform.Text(claim, "user_id") != "demo" ||
		platform.Text(claim, "template_id") != "integration-points" ||
		claim["already_claimed"] != false {
		t.Fatalf("claim did not create an owned coupon: %v", claim)
	}
	if points := h.call(token, "points", "get", "", "", nil); platform.Int(
		points,
		"available",
	) != platform.Int(
		qualifiedPoints,
		"available",
	) {
		t.Fatalf("checking points eligibility spent points: before=%v after=%v", qualifiedPoints, points)
	}
	assertStatus(allPages(token), "integration-points", "claimed")
	assertStatus(allPages(""), "integration-points", "login_required")
	var remaining int64
	if err := c.dbs["coupon"].Table("templates").Select("remaining").Where("id = ?", "integration-points").Scan(
		&remaining,
	).Error; err != nil ||
		remaining != 4 {
		t.Fatalf("first claim did not consume exactly one quota: remaining=%d error=%v", remaining, err)
	}
	if err := c.dbs["coupon"].Table("templates").Where("id = ?", "integration-points").Updates(
		map[string]any{"active": false, "expires_at": now.Add(-time.Hour), "remaining": 0, "min_points": 999999},
	).Error; err != nil {
		t.Fatal(err)
	}
	retry := h.call(token, "coupon", "claim", "", "", map[string]any{"template_id": "integration-points"})
	if platform.Text(retry, "id") != couponID || retry["already_claimed"] != true {
		t.Fatalf("retry after policy closure did not recover the original coupon: %v", retry)
	}
	var claims int64
	if err := c.dbs["coupon"].Table("claims").Where(
		"user_id = ? AND template_id = ?",
		"demo",
		"integration-points",
	).Count(
		&claims,
	).Error; err != nil ||
		claims != 1 {
		t.Fatalf("claim retry created duplicate entitlements: claims=%d error=%v", claims, err)
	}
	walletState := func(want string) {
		t.Helper()
		for _, item := range mapItems(h.call(token, "coupon", "list", "", "", nil)) {
			if platform.Text(item, "id") == couponID {
				if platform.Text(item, "status") != want {
					t.Fatalf("wallet state want %s, got %v", want, item)
				}
				return
			}
		}
		t.Fatalf("claimed coupon %s disappeared from its owner's wallet", couponID)
	}
	walletState("AVAILABLE")
	quote := h.call(
		token,
		"pricing",
		"quote",
		"",
		"",
		map[string]any{"coupon_id": couponID, "items": []any{map[string]any{"sku_id": "sku-watch", "quantity": 1}}},
	)
	if platform.Int(quote, "coupon_discount_cents") != 1000 {
		t.Fatalf("claimed coupon did not reach authoritative quote: %v", quote)
	}
	placed := h.call(
		token,
		"checkout",
		"submit",
		"",
		"coupon-center-paid",
		map[string]any{
			"quote_id": quote["quote_id"],
			"address": map[string]any{
				"recipient": "Coupon Buyer",
				"phone":     "13800000000",
				"address":   "Integration Coupon Street",
			},
		},
	)
	orderID := platform.Text(placed, "order_id")
	walletState("HELD")
	payment := h.call(token, "payment", "create", orderID, "", nil)
	h.call(token, "payment", "submit_demo", platform.Text(payment, "id"), "", nil)
	c.pump(t)
	walletState("USED")
	order := h.call(token, "order", "get", orderID, "", nil)
	if platform.Text(order, "status") != "READY_TO_FULFILL" ||
		platform.Int(order, "payable_cents") != platform.Int(quote, "payable_cents") {
		t.Fatalf("claimed coupon checkout did not complete payment: %v", order)
	}
	if warehouse := h.call(token, "fulfillment", "get", orderID, "", nil); platform.Text(
		warehouse,
		"status",
	) != "READY" {
		t.Fatalf("coupon order did not enter fulfillment: %v", warehouse)
	}
	admin := platform.Text(
		h.call("", "iam", "login", "", "", map[string]any{"username": "admin", "password": "Admin@2026"}),
		"token",
	)
	after := h.call(
		token,
		"aftersale",
		"create",
		"",
		"coupon-center-refund",
		map[string]any{"order_id": orderID, "type": "refund", "reason": "Verify claimed coupon full refund"},
	)
	afterID := platform.Text(after, "id")
	h.call(admin, "aftersale", "approve", afterID, "", nil)
	refund := h.call(admin, "aftersale", "refund", afterID, "", nil)
	h.call(admin, "refund", "confirm_demo", platform.Text(refund, "refund_id"), "", nil)
	c.pump(t)
	walletState("AVAILABLE")
	if refunded := h.call(token, "order", "get", orderID, "", nil); platform.Int(
		refunded,
		"refunded_total_cents",
	) != platform.Int(
		quote,
		"payable_cents",
	) {
		t.Fatalf("coupon order refund did not settle actual paid cash: %v", refunded)
	}
	// A consumed or returned coupon never creates a second claim entitlement.
	if final := h.call(
		token,
		"coupon",
		"claim",
		"",
		"",
		map[string]any{"template_id": "integration-points"},
	); platform.Text(
		final,
		"id",
	) != couponID {
		t.Fatalf("payment/refund allowed another coupon claim: %v", final)
	}
}
