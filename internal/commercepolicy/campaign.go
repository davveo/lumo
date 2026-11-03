package commercepolicy

import "github.com/davveo/lumo/internal/constants"

type Campaign struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Threshold   int64  `json:"threshold_cents"`
	Discount    int64  `json:"discount_cents"`
	PayRateBPS  int64  `json:"pay_rate_bps"`
	MinQuantity int64  `json:"min_quantity"`
	Quota       int64  `json:"quota"`
	Held        int64  `json:"held"`
	Used        int64  `json:"used"`
	Active      bool   `json:"active"`
}

func EvaluateCampaign(a Campaign, subtotal, quantity int64) (int64, string, string) {
	policy := "v1"
	modern := a.Kind == constants.CampaignKindReduction || a.Kind == constants.CampaignKindDiscount ||
		a.Kind == constants.CampaignKindQuantityDiscount ||
		a.Kind == constants.CampaignKindDirect
	if modern {
		policy = "v2"
	}
	reject := func(reason string) (int64, string, string) { return 0, reason, policy }
	if !a.Active {
		return reject("inactive")
	}
	if a.Threshold < 0 || a.Discount < 0 || a.Held < 0 || a.Used < 0 || a.Quota < 1 {
		return reject("invalid_rule")
	}
	if subtotal < a.Threshold {
		return reject("threshold_not_met")
	}
	if modern && (a.MinQuantity < 1 || quantity < a.MinQuantity) {
		return reject("quantity_not_met")
	}
	if a.Held >= a.Quota || a.Used >= a.Quota-a.Held {
		return reject("quota_exhausted")
	}
	discount := a.Discount
	switch a.Kind {
	case constants.CampaignKindFlash, constants.CampaignKindGroup, constants.CampaignKindAddon:
		// Preserve the existing fixed-discount rules and their zero-discount
		// flash/group quota contracts.
	case constants.CampaignKindReduction, constants.CampaignKindDirect:
		if discount < 1 {
			return reject("invalid_rule")
		}
	case constants.CampaignKindDiscount, constants.CampaignKindQuantityDiscount:
		if a.PayRateBPS < 1 || a.PayRateBPS >= 10000 {
			return reject("invalid_rule")
		}
		// Split the multiplication so public preview amounts cannot overflow.
		off := int64(10000) - a.PayRateBPS
		discount = subtotal/10000*off + subtotal%10000*off/10000
	default:
		return reject("invalid_rule")
	}
	if modern && (discount < 1 || discount >= subtotal) {
		return reject("amount_too_small")
	}
	return discount, "", policy
}
