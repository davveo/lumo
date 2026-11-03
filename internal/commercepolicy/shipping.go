package commercepolicy

import (
	"strings"
	"time"
)

type RegionFee struct {
	Region        string `json:"region"`
	BaseFee       int64  `json:"base_fee_cents"`
	AdditionalFee int64  `json:"additional_fee_cents"`
	FreeThreshold int64  `json:"free_threshold_cents"`
}
type Shipping struct {
	PromiseEnabled bool        `json:"promise_enabled"`
	Warehouse      string      `json:"warehouse"`
	HandlingDays   int         `json:"handling_days"`
	CutoffHour     int         `json:"cutoff_hour"`
	TransitMinDays int         `json:"transit_min_days"`
	TransitMaxDays int         `json:"transit_max_days"`
	Version        int64       `json:"version"`
	Name           string      `json:"name"`
	Enabled        bool        `json:"enabled"`
	Mode           string      `json:"mode"`
	BaseFee        int64       `json:"base_fee_cents"`
	AdditionalFee  int64       `json:"additional_fee_cents"`
	FreeThreshold  int64       `json:"free_threshold_cents"`
	AllowedRegions []string    `json:"allowed_regions"`
	BlockedRegions []string    `json:"blocked_regions"`
	Regions        []RegionFee `json:"regions"`
}

// Promise is a calendar-day estimate from configured warehouse rules, never a
// carrier guarantee. A selected region and explicit configuration are required.
func (p Shipping) Promise(region string, now time.Time) map[string]any {
	if region == "" {
		return map[string]any{"available": false, "reason": "选择收货地址后查询配送预估"}
	}
	if !p.PromiseEnabled {
		return map[string]any{"available": false, "reason": "商家暂未配置配送时效，请咨询客服"}
	}
	if _, ok := p.Quote(region, 0, 1); !ok {
		return map[string]any{"available": false, "reason": "当前地址暂不可配送"}
	}
	local := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	handling := p.HandlingDays
	if local.Hour() >= p.CutoffHour {
		handling++
	}
	dispatch := day.AddDate(0, 0, handling)
	return map[string]any{
		"available":      true,
		"warehouse":      p.Warehouse,
		"dispatch_by":    dispatch.Format("2006-01-02"),
		"delivery_from":  dispatch.AddDate(0, 0, p.TransitMinDays).Format("2006-01-02"),
		"delivery_to":    dispatch.AddDate(0, 0, p.TransitMaxDays).Format("2006-01-02"),
		"basis":          "按自然日估算，实际以物流轨迹为准",
		"policy_version": p.Version,
	}
}

func DefaultShipping() Shipping {
	return Shipping{
		Name:           "标准配送",
		Enabled:        true,
		Mode:           "flat",
		AllowedRegions: []string{},
		BlockedRegions: []string{},
		Regions:        []RegionFee{},
	}
}
func Region(address map[string]any) string {
	parts := []string{}
	for _, key := range []string{"province", "city", "district"} {
		if v, ok := address[key].(string); ok && strings.TrimSpace(v) != "" {
			parts = append(parts, strings.Join(strings.Fields(v), ""))
		}
	}
	if len(parts) == 0 {
		if v, ok := address["region"].(string); ok {
			parts = append(parts, strings.Join(strings.Fields(v), ""))
		}
	}
	return strings.Join(parts, "")
}
func (p Shipping) Quote(region string, subtotal, quantity int64) (int64, bool) {
	if !p.Enabled {
		return 0, false
	}
	has := func(regions []string) bool {
		for _, v := range regions {
			if strings.Contains(region, strings.Join(strings.Fields(v), "")) {
				return true
			}
		}
		return false
	}
	if has(p.BlockedRegions) || (len(p.AllowedRegions) > 0 && !has(p.AllowedRegions)) {
		return 0, false
	}
	base, extra, free := p.BaseFee, p.AdditionalFee, p.FreeThreshold
	for _, r := range p.Regions {
		if strings.Contains(region, strings.Join(strings.Fields(r.Region), "")) {
			base, extra, free = r.BaseFee, r.AdditionalFee, r.FreeThreshold
			break
		}
	}
	if free > 0 && subtotal >= free {
		return 0, true
	}
	if quantity < 1 {
		quantity = 1
	}
	if p.Mode == "per_item" {
		base += (quantity - 1) * extra
	}
	return base, true
}
