// Package couponrules shares immutable coupon-benefit calculations between
// quotation and reservation. It has no access to any service's database.
package couponrules

import (
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"reflect"
	"sort"
	"strings"
)

type Rule struct {
	CouponType  string   `gorm:"size:32" json:"coupon_type"`
	DiscountBPS int64    `json:"discount_bps"`
	MaxDiscount int64    `json:"max_discount_cents"`
	ScopeType   string   `gorm:"size:24" json:"scope_type"`
	ScopeValues []string `gorm:"serializer:json;type:text" json:"scope_values"`
}
type Line struct {
	SKUID     string `json:"sku_id"`
	ProductID string `json:"product_id"`
	Category  string `json:"category"`
	Amount    int64  `json:"amount_cents"`
}

func (r *Rule) Normalize(amount, minimum int64) error {
	if r.CouponType == "" {
		r.CouponType = constants.CouponTypeReduction
		if minimum == 0 {
			r.CouponType = constants.CouponTypeCash
		}
	}
	if r.ScopeType == "" {
		r.ScopeType = constants.CouponScopeAll
	}
	if minimum < 0 || minimum > 9007199254740991 || amount < 0 || amount > 9007199254740991 || r.MaxDiscount < 0 ||
		r.MaxDiscount > 9007199254740991 {
		return fmt.Errorf("invalid coupon benefit amount")
	}
	switch r.CouponType {
	case constants.CouponTypeCash:
		if minimum != 0 || amount < 1 {
			return fmt.Errorf("cash coupon requires a positive amount and no threshold")
		}
	case constants.CouponTypeReduction:
		if amount < 1 || minimum < 1 {
			return fmt.Errorf("reduction coupon requires a positive amount and threshold")
		}
	case constants.CouponTypeDiscount, constants.CouponTypeThresholdDiscount:
		if r.DiscountBPS < 1 || r.DiscountBPS >= 10000 || amount != 0 {
			return fmt.Errorf("discount coupon requires 1..9999 basis points and zero fixed amount")
		}
		if r.CouponType == constants.CouponTypeDiscount && minimum != 0 ||
			r.CouponType == constants.CouponTypeThresholdDiscount && minimum < 1 {
			return fmt.Errorf("discount coupon threshold does not match its type")
		}
	default:
		return fmt.Errorf("unsupported coupon type")
	}
	if r.CouponType != constants.CouponTypeDiscount && r.CouponType != constants.CouponTypeThresholdDiscount &&
		(r.DiscountBPS != 0 || r.MaxDiscount != 0) {
		return fmt.Errorf("fixed coupon cannot carry discount rate or cap")
	}
	if len(r.ScopeValues) > 50 {
		return fmt.Errorf("coupon scope has too many values")
	}
	values := []string{}
	seen := map[string]bool{}
	for _, value := range r.ScopeValues {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 150 || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("invalid coupon scope value")
		}
		if !seen[value] {
			values = append(values, value)
			seen[value] = true
		}
	}
	sort.Strings(values)
	r.ScopeValues = values
	switch r.ScopeType {
	case constants.CouponScopeAll:
		if len(values) > 0 {
			return fmt.Errorf("all-product coupon cannot restrict specific values")
		}
	case constants.CouponScopeCategory, constants.CouponScopeProduct:
		if len(values) == 0 {
			return fmt.Errorf("restricted coupon requires categories or product IDs")
		}
	default:
		return fmt.Errorf("unsupported coupon scope")
	}
	return nil
}
func Same(a, b Rule, amountA, minA, amountB, minB int64) bool {
	if a.Normalize(amountA, minA) != nil || b.Normalize(amountB, minB) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}
func Discount(rule Rule, amount, minimum, subtotal int64, items []Line) (int64, error) {
	if err := rule.Normalize(amount, minimum); err != nil {
		return 0, err
	}
	if subtotal < 1 || subtotal > 9007199254740991 {
		return 0, fmt.Errorf("invalid coupon subtotal")
	}
	eligible := subtotal
	if rule.ScopeType != constants.CouponScopeAll {
		if len(items) < 1 || len(items) > 50 {
			return 0, fmt.Errorf("coupon requires authoritative product lines")
		}
		eligible = 0
		var sum int64
		for _, line := range items {
			if line.Amount < 0 || line.Amount > subtotal-sum {
				return 0, fmt.Errorf("invalid coupon item amount")
			}
			sum += line.Amount
			value := line.Category
			if rule.ScopeType == constants.CouponScopeProduct {
				value = line.ProductID
				if value == "" {
					value = line.SKUID
				}
			}
			for _, allowed := range rule.ScopeValues {
				if value == allowed {
					eligible += line.Amount
					break
				}
			}
		}
		if sum != subtotal {
			return 0, fmt.Errorf("coupon lines do not match main subtotal")
		}
	}
	if eligible < 1 || eligible < minimum {
		return 0, fmt.Errorf("coupon scope or spending threshold not met")
	}
	value := amount
	if rule.CouponType == constants.CouponTypeDiscount || rule.CouponType == constants.CouponTypeThresholdDiscount {
		rate := 10000 - rule.DiscountBPS
		value = eligible/10000*rate + eligible%10000*rate/10000
		if rule.MaxDiscount > 0 && value > rule.MaxDiscount {
			value = rule.MaxDiscount
		}
	}
	if value > eligible {
		value = eligible
	}
	if value < 1 {
		return 0, fmt.Errorf("coupon discount rounds to zero")
	}
	return value, nil
}
