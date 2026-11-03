package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/couponrules"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
)

// Optimize loads authoritative snapshots before the comparison loops. Neither
// comparing offers nor evaluating top-ups makes RPCs or spends member assets.
func (s *Service) Optimize(ctx context.Context, c platform.Request) (platform.Result, error) {
	baseline, err := s.Quote(ctx, c)
	if err != nil {
		return nil, err
	}
	if platform.Text(baseline, "group_id") != "" || platform.Text(baseline, "addon_sku_id") != "" ||
		platform.Text(baseline, "activity_kind") == constants.CampaignKindFlash {
		return platform.Result{"quote": baseline, "savings_cents": 0, "restricted": true, "topups": []any{}}, nil
	}
	clients := platform.NewClients(s.rpc)
	activities, err := clients.Campaign.List(ctx, platform.Request{UserID: c.UserID, Role: c.Role})
	if err != nil {
		return nil, err
	}
	wallet, err := clients.Coupon.List(ctx, platform.Request{UserID: c.UserID, Role: c.Role})
	if err != nil {
		return nil, err
	}
	catalog, err := clients.Catalog.List(ctx, platform.Request{Data: map[string]any{"limit": 30}})
	if err != nil {
		return nil, err
	}
	var aa struct {
		Items []commercepolicy.Campaign `json:"items"`
	}
	var cc struct {
		Items []map[string]any `json:"items"`
	}
	var pp struct {
		Items []platform.Result `json:"items"`
	}
	if err = platform.Decode(activities, &aa); err != nil {
		return nil, err
	}
	if err = platform.Decode(wallet, &cc); err != nil {
		return nil, err
	}
	if err = platform.Decode(catalog, &pp); err != nil {
		return nil, err
	}
	if len(aa.Items) > 1000 {
		return nil, status.Error(codes.ResourceExhausted, "too many promotion rules")
	}
	var original struct {
		Items    []couponrules.Line `json:"coupon_items"`
		Shipping struct {
			Policy commercepolicy.Shipping `json:"policy"`
		} `json:"shipping"`
	}
	if err = platform.Decode(baseline, &original); err != nil {
		return nil, err
	}
	var cart struct {
		Items []struct {
			SKU      string `json:"sku_id"`
			Quantity int64  `json:"quantity"`
		} `json:"items"`
	}
	_ = platform.Decode(c.Data, &cart)
	subtotal := platform.Int(baseline, "subtotal_cents")
	quantity := int64(0)
	seen := map[string]bool{}
	for _, line := range cart.Items {
		quantity += line.Quantity
		seen[line.SKU] = true
	}
	ids := []string{}
	for _, p := range pp.Items {
		if id := platform.Text(p, "id"); id != "" && !seen[id] {
			ids = append(ids, id)
		}
	}
	stocks := map[string]platform.Result{}
	if len(ids) > 0 {
		value, e := clients.Inventory.BatchGet(ctx, platform.Request{Data: map[string]any{"ids": ids}})
		if e != nil {
			return nil, e
		}
		stocks, err = platform.IndexResults(value, "sku_id")
		if err != nil {
			return nil, err
		}
	}
	rules := []commercepolicy.Campaign{{Active: true}}
	for _, a := range aa.Items {
		if a.Kind == constants.CampaignKindReduction || a.Kind == constants.CampaignKindDiscount ||
			a.Kind == constants.CampaignKindQuantityDiscount ||
			a.Kind == constants.CampaignKindDirect {
			rules = append(rules, a)
		}
	}
	points := platform.Int(baseline, "points")
	region := platform.Text(baseline, "shipping_region")
	evaluate := func(sub, qty int64, lines []couponrules.Line) platform.Result {
		best := platform.Result{"payable_cents": int64(1 << 62)}
		for _, a := range rules {
			discount, why := int64(0), ""
			if a.ID != "" {
				discount, why, _ = commercepolicy.EvaluateCampaign(a, sub, qty)
			}
			if why != "" {
				continue
			}
			cid, off, _, e := chooseCouponRows(
				platform.Text(c.Data, "coupon_mode"),
				platform.Text(c.Data, "coupon_id"),
				sub,
				sub-discount,
				lines,
				cc.Items,
			)
			if e != nil {
				continue
			}
			if sub-discount-off-points < 1 {
				continue
			}
			fee, allowed := original.Shipping.Policy.Quote(region, sub, qty)
			if !allowed {
				continue
			}
			payable := sub - discount - off - points + fee
			if payable < platform.Int(best, "payable_cents") {
				best = platform.Result{
					"activity_id":             a.ID,
					"activity_name":           a.Name,
					"coupon_id":               cid,
					"payable_cents":           payable,
					"campaign_discount_cents": discount,
					"coupon_discount_cents":   off,
					"shipping_cents":          fee,
					"points":                  points,
				}
			}
		}
		return best
	}
	best := evaluate(subtotal, quantity, original.Items)
	if platform.Int(best, "payable_cents") < platform.Int(baseline, "payable_cents") {
		data := map[string]any{}
		for k, v := range c.Data {
			data[k] = v
		}
		data["activity_id"] = best["activity_id"]
		data["coupon_id"] = best["coupon_id"]
		data["coupon_mode"] = "manual"
		// Revalidate the selected solution once; concurrent changes never replace a
		// valid baseline with a more expensive recommendation.
		fresh, e := s.Quote(ctx, platform.Request{UserID: c.UserID, Role: c.Role, Data: data})
		if e != nil {
			return nil, e
		}
		if platform.Int(fresh, "payable_cents") <= platform.Int(baseline, "payable_cents") {
			best = fresh
		} else {
			best = baseline
		}
	} else {
		best = baseline
	}
	topups := []platform.Result{}
	if len(cart.Items) < 50 {
		for _, p := range pp.Items {
			id := platform.Text(p, "id")
			unit := platform.Int(p, "price_cents")
			if seen[id] || unit < 1 || unit > 10000000000 || platform.Int(stocks[id], "available") < 1 {
				continue
			}
			unlocks := false
			gap := int64(0)
			for _, a := range rules {
				if a.Active &&
					((a.Threshold > subtotal && a.Threshold <= subtotal+unit && a.MinQuantity <= quantity+1) || (a.MinQuantity > quantity && a.MinQuantity <= quantity+1 && a.Threshold <= subtotal+unit)) {
					unlocks = true
					gap = max(int64(0), a.Threshold-subtotal)
				}
			}
			for _, coupon := range cc.Items {
				need := platform.Int(coupon, "min_cents")
				if need == 0 {
					need = platform.Int(coupon, "min_spend_cents")
				}
				if need > subtotal && need <= subtotal+unit {
					unlocks = true
					gap = need - subtotal
				}
			}
			free := original.Shipping.Policy.FreeThreshold
			if free > subtotal && free <= subtotal+unit {
				unlocks = true
				gap = free - subtotal
			}
			if !unlocks {
				continue
			}
			product := platform.Text(p, "product_id")
			if product == "" {
				product = id
			}
			lines := append(
				append([]couponrules.Line{}, original.Items...),
				couponrules.Line{SKUID: id, ProductID: product, Category: platform.Text(p, "category"), Amount: unit},
			)
			solution := evaluate(subtotal+unit, quantity+1, lines)
			extra := platform.Int(solution, "payable_cents") - platform.Int(best, "payable_cents")
			benefit := unit - extra
			if benefit <= 0 {
				continue
			}
			topups = append(
				topups,
				platform.Result{
					"sku_id":                   id,
					"name":                     p["name"],
					"quantity":                 1,
					"price_cents":              unit,
					"threshold_gap_cents":      gap,
					"additional_payable_cents": extra,
					"additional_savings_cents": benefit,
					"estimated":                true,
				},
			)
		}
	}
	sort.SliceStable(topups, func(i, j int) bool {
		return platform.Int(topups[i], "additional_payable_cents") < platform.Int(topups[j], "additional_payable_cents")
	})
	if len(topups) > 3 {
		topups = topups[:3]
	}
	return platform.Result{
		"quote":                  best,
		"baseline_payable_cents": baseline["payable_cents"],
		"savings_cents": max(
			int64(0),
			platform.Int(baseline, "payable_cents")-platform.Int(best, "payable_cents"),
		),
		"topups":                      topups,
		"points_require_confirmation": true,
	}, nil
}
