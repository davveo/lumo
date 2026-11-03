package commercepolicy

import "strings"

type Aftersale struct {
	Version           int64  `json:"version"`
	Configured        bool   `json:"configured"`
	ReviewHours       int    `json:"review_hours"`
	RefundSubmitHours int    `json:"refund_submit_hours"`
	ReturnRecipient   string `json:"return_recipient"`
	ReturnPhone       string `json:"return_phone"`
	ReturnAddress     string `json:"return_address"`
	Instructions      string `json:"instructions"`
}

func DefaultAftersale() Aftersale { return Aftersale{ReviewHours: 48, RefundSubmitHours: 24} }
func (p Aftersale) Valid() bool {
	return p.ReviewHours >= 0 && p.ReviewHours <= 720 && p.RefundSubmitHours >= 0 && p.RefundSubmitHours <= 720 &&
		len([]rune(p.ReturnRecipient)) <= 80 &&
		len(p.ReturnPhone) <= 40 &&
		len([]rune(p.ReturnAddress)) <= 500 &&
		len([]rune(p.Instructions)) <= 1000 &&
		(!p.Configured || (p.ReviewHours > 0 && p.RefundSubmitHours > 0 && strings.TrimSpace(p.Instructions) != ""))
}
