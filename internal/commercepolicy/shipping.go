package commercepolicy

import (
	"strings"
)

type RegionFee struct {
	Region        string `json:"region"`
	BaseFee       int64  `json:"base_fee_cents"`
	AdditionalFee int64  `json:"additional_fee_cents"`
	FreeThreshold int64  `json:"free_threshold_cents"`
}
type Shipping struct {
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

func DefaultShipping() Shipping {
	return Shipping{Name: "标准配送", Enabled: true, Mode: "flat", AllowedRegions: []string{}, BlockedRegions: []string{}, Regions: []RegionFee{}}
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
