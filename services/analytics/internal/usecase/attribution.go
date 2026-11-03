package usecase

import (
	"encoding/json"
	"github.com/davveo/lumo/internal/attribution"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/domain"
)

func attachAttribution(row *domain.TransactionFact, order platform.Result) {
	if order == nil {
		return
	}
	var quote map[string]any
	if raw, ok := order["quote"].(string); ok {
		_ = json.Unmarshal([]byte(raw), &quote)
	} else {
		b, _ := json.Marshal(order["quote"])
		_ = json.Unmarshal(b, &quote)
	}
	if quote == nil {
		return
	}
	var in struct {
		Attribution attribution.Context `json:"attribution"`
	}
	_ = platform.Decode(quote, &in)
	if in.Attribution.Valid() {
		in.Attribution = in.Attribution.Normalized()
		row.SessionKey = in.Attribution.SessionKey()
		row.Source = in.Attribution.Source
		row.SearchQuery = in.Attribution.Query
	}
	_, hasCampaign := quote["campaign_discount_cents"]
	_, hasCoupon := quote["coupon_discount_cents"]
	_, hasPoints := quote["points"]
	row.CostKnown = hasCampaign && hasCoupon && hasPoints
	row.CampaignCost = platform.Int(quote, "campaign_discount_cents")
	row.CouponCost = platform.Int(quote, "coupon_discount_cents")
	row.PointsCost = platform.Int(quote, "points")
}
