package commercepolicy

import "strings"

// ProductService is snapshotted with each priced line; unset terms remain
// explicitly unspecified instead of implying an unsupported return guarantee.
type ProductService struct {
	Configured     bool   `json:"configured"`
	ReturnDays     int    `json:"return_days"`
	ReturnShipping string `json:"return_shipping"`
	WarrantyMonths int    `json:"warranty_months"`
	Explanation    string `json:"explanation"`
}

func (p ProductService) Valid() bool {
	if !p.Configured {
		return true
	}
	return p.ReturnDays >= 0 && p.ReturnDays <= 365 && p.WarrantyMonths >= 0 && p.WarrantyMonths <= 120 &&
		len([]rune(p.Explanation)) <= 500 &&
		(p.ReturnShipping == "buyer" || p.ReturnShipping == "seller" || p.ReturnShipping == "case_by_case") &&
		strings.TrimSpace(p.Explanation) != ""
}
