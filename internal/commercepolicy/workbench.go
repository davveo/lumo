package commercepolicy

// Workbench targets are internal follow-up goals, not customer delivery or
// payment-channel arrival guarantees.
type Workbench struct {
	Version        int64 `json:"version"`
	PaymentHours   int   `json:"payment_hours"`
	RefundHours    int   `json:"refund_hours"`
	ShippingHours  int   `json:"shipping_hours"`
	AftersaleHours int   `json:"aftersale_hours"`
	SupportHours   int   `json:"support_hours"`
}

func DefaultWorkbench() Workbench {
	return Workbench{PaymentHours: 2, RefundHours: 24, ShippingHours: 24, AftersaleHours: 48, SupportHours: 24}
}
func (p Workbench) Valid() bool {
	return p.PaymentHours >= 0 && p.PaymentHours <= 720 && p.RefundHours >= 0 && p.RefundHours <= 720 &&
		p.ShippingHours >= 0 &&
		p.ShippingHours <= 720 &&
		p.AftersaleHours >= 0 &&
		p.AftersaleHours <= 720 &&
		p.SupportHours >= 0 &&
		p.SupportHours <= 720
}
