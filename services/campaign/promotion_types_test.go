package campaign_test

import (
	"context"
	"math"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPromotionKindsUseServerRulesAndIntegerCents(t *testing.T) {
	_, m := setup(t, nil)
	for _, test := range []struct {
		name, activity, kind string
		subtotal, quantity   int64
		discount             int64
		eligible             bool
		reason               string
	}{
		{"reduction below threshold", "reduction-demo", "reduction", 29999, 1, 0, false, "threshold_not_met"},
		{"reduction at threshold", "reduction-demo", "reduction", 30000, 1, 3000, true, ""},
		{"nine tenths cents rounded conservatively", "discount-demo", "discount", 101, 1, 10, true, ""},
		{"discount cannot create a zero cent benefit", "discount-demo", "discount", 1, 1, 0, false, "amount_too_small"},
		{"discount preview cannot overflow", "discount-demo", "discount", math.MaxInt64, 1, math.MaxInt64 / 10, true, ""},
		{"quantity discount needs two units", "quantity-discount-demo", "quantity_discount", 20000, 1, 0, false, "quantity_not_met"},
		{"quantity discount at two units", "quantity-discount-demo", "quantity_discount", 202, 2, 30, true, ""},
		{"missing quantity cannot qualify", "quantity-discount-demo", "quantity_discount", 20000, 0, 0, false, "quantity_not_met"},
		{"direct does not erase the order", "direct-demo", "direct", 2000, 1, 0, false, "amount_too_small"},
		{"direct keeps at least one cent payable", "direct-demo", "direct", 2001, 1, 2000, true, ""},
		{"legacy add-on unchanged without quantity", "addon-coffee", "addon", 100000, 0, 6000, true, ""},
		{"legacy flash unchanged", "flash-demo", "flash", 100, 0, 0, true, ""},
		{"legacy group unchanged", "group-demo", "group", 100, 0, 0, true, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := must(t, m, "evaluate", "", "demo", "user", map[string]any{"activity_id": test.activity, "subtotal_cents": test.subtotal, "quantity": test.quantity})
			if result["eligible"] != test.eligible || platform.Int(result, "discount_cents") != test.discount || platform.Text(result, "kind") != test.kind || platform.Text(result, "reason") != test.reason {
				t.Fatalf("unexpected campaign decision: %v", result)
			}
			if test.kind != "addon" && test.kind != "flash" && test.kind != "group" {
				if platform.Text(result, "name") == "" || platform.Text(result, "policy_version") != "v2" {
					t.Fatalf("missing versioned activity metadata: %v", result)
				}
			}
		})
	}
	plain := must(t, m, "evaluate", "", "", "", map[string]any{"subtotal_cents": 50000, "quantity": 2})
	if plain["eligible"] != true || platform.Int(plain, "discount_cents") != 0 || platform.Text(plain, "activity_id") != "" {
		t.Fatalf("ordinary purchases must not automatically gain a campaign: %v", plain)
	}
	for _, input := range []map[string]any{{"subtotal_cents": -1}, {"subtotal_cents": 20000, "quantity": -1}} {
		if _, err := invoke(m, "evaluate", "", "", "", input); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid preview accepted: %v", err)
		}
	}
}

func TestPromotionSeedsPreserveExistingConfigurationAndUsage(t *testing.T) {
	db, m := setup(t, nil)
	if err := db.Table("activities").Where("id = ?", "discount-demo").Updates(map[string]any{"quota": 7, "used": 2, "name": "运营保留的活动名称"}).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.Seed(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Table("activities").Count(&count).Error; err != nil || count != 7 {
		t.Fatalf("seed should create exactly four new types: %d %v", count, err)
	}
	var configured struct {
		Quota, Used int64
		Name        string
	}
	if err := db.Table("activities").Where("id = ?", "discount-demo").Take(&configured).Error; err != nil {
		t.Fatal(err)
	}
	if configured.Quota != 7 || configured.Used != 2 || configured.Name != "运营保留的活动名称" {
		t.Fatalf("seed overwrote an existing campaign: %+v", configured)
	}
}

func TestNewPromotionKindsRetainQuotaAndIdempotency(t *testing.T) {
	for _, activity := range []string{"reduction-demo", "discount-demo", "quantity-discount-demo", "direct-demo"} {
		t.Run(activity, func(t *testing.T) {
			db, m := setup(t, nil)
			reserve := map[string]any{"activity_id": activity, "quantity": 1}
			for range 2 {
				must(t, m, "reserve", "order-cancel", "buyer", "system", reserve)
			}
			var usage struct{ Held, Used int64 }
			readUsage := func(held, used int64) {
				t.Helper()
				if err := db.Table("activities").Where("id = ?", activity).Take(&usage).Error; err != nil {
					t.Fatal(err)
				}
				if usage.Held != held || usage.Used != used {
					t.Fatalf("duplicate quota mutation: %+v", usage)
				}
			}
			readUsage(1, 0)
			for range 2 {
				must(t, m, "cancel", "order-cancel", "buyer", "system", nil)
			}
			readUsage(0, 0)
			must(t, m, "reserve", "order-paid", "buyer", "system", reserve)
			for range 2 {
				must(t, m, "confirm", "order-paid", "buyer", "system", nil)
			}
			readUsage(0, 1)
			if _, err := invoke(m, "reserve", "order-another", "buyer", "system", reserve); status.Code(err) != codes.ResourceExhausted {
				t.Fatalf("per-member campaign limit bypassed: %v", err)
			}
		})
	}
}

func TestDisabledExhaustedAndInvalidDiscountRulesNeverQualify(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		updates      map[string]any
	}{
		{"disabled", "inactive", map[string]any{"active": false}},
		{"exhausted", "quota_exhausted", map[string]any{"used": 1000}},
		{"invalid rate", "invalid_rule", map[string]any{"pay_rate_bps": 10000}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, m := setup(t, nil)
			if err := db.Table("activities").Where("id = ?", "discount-demo").Updates(test.updates).Error; err != nil {
				t.Fatal(err)
			}
			result := must(t, m, "evaluate", "", "buyer", "user", map[string]any{"activity_id": "discount-demo", "subtotal_cents": 10000, "quantity": 1})
			if result["eligible"] != false || platform.Int(result, "discount_cents") != 0 || platform.Text(result, "reason") != test.reason {
				t.Fatalf("bad rule qualified: %v", result)
			}
		})
	}
}
