package couponrules

import (
	"github.com/davveo/lumo/internal/constants"
	"testing"
)

func TestCouponTypesScopesCapsAndExactRounding(t *testing.T) {
	items := []Line{
		{SKUID: "sku-a", ProductID: "a", Category: "家电", Amount: 12000},
		{SKUID: "sku-b", ProductID: "b", Category: "数码", Amount: 8000},
	}
	for _, tc := range []struct {
		name              string
		rule              Rule
		amount, min, want int64
	}{
		{"满减", Rule{}, 2000, 15000, 2000}, {"无门槛", Rule{CouponType: constants.CouponTypeCash}, 500, 0, 500}, {"八五折", Rule{CouponType: constants.CouponTypeDiscount, DiscountBPS: 8500}, 0, 0, 3000}, {"封顶", Rule{CouponType: constants.CouponTypeThresholdDiscount, DiscountBPS: 8500, MaxDiscount: 1500}, 0, 10000, 1500}, {"品类", Rule{CouponType: constants.CouponTypeDiscount, DiscountBPS: 9000, ScopeType: constants.CouponScopeCategory, ScopeValues: []string{"家电"}}, 0, 0, 1200}, {"商品", Rule{ScopeType: constants.CouponScopeProduct, ScopeValues: []string{"b"}}, 1000, 5000, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Discount(tc.rule, tc.amount, tc.min, 20000, items)
			if err != nil || got != tc.want {
				t.Fatalf("got %d want %d err=%v", got, tc.want, err)
			}
		})
	}
	got, err := Discount(Rule{CouponType: constants.CouponTypeDiscount, DiscountBPS: 9999}, 0, 0, 9007199254740991, nil)
	if err != nil || got != 900719925474 {
		t.Fatalf("large integer exact calculation: %d %v", got, err)
	}
}
func TestCouponRulesRejectInvalidOrForgedScopes(t *testing.T) {
	for _, rule := range []Rule{{CouponType: "unknown"}, {CouponType: constants.CouponTypeDiscount, DiscountBPS: 10000}, {ScopeType: constants.CouponScopeProduct}, {ScopeType: constants.CouponScopeAll, ScopeValues: []string{"a"}}} {
		if rule.Normalize(10, 0) == nil {
			t.Fatalf("invalid rule accepted: %+v", rule)
		}
	}
	rule := Rule{ScopeType: constants.CouponScopeCategory, ScopeValues: []string{"家电"}}
	if _, err := Discount(rule, 100, 0, 1000, []Line{{Category: "家电", Amount: 500}}); err == nil {
		t.Fatal("forged scoped subtotal accepted")
	}
	if _, err := Discount(rule, 100, 0, 1000, []Line{{Category: "数码", Amount: 1000}}); err == nil {
		t.Fatal("unmatched category accepted")
	}
	if !Same(Rule{}, Rule{CouponType: constants.CouponTypeCash, ScopeType: constants.CouponScopeAll}, 500, 0, 500, 0) {
		t.Fatal("legacy rule snapshot changed")
	}
}
