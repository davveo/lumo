package domain

import "time"

type Record struct {
	ID             string    `gorm:"primaryKey;size:100" json:"id"`
	UserID         string    `gorm:"index;size:80" json:"user_id"`
	OrderID        string    `gorm:"index;size:100" json:"order_id"`
	PaymentID      string    `gorm:"size:100" json:"payment_id"`
	Type           string    `gorm:"size:30" json:"type"`
	Reason         string    `gorm:"size:1000" json:"reason"`
	Status         string    `gorm:"index;size:40" json:"status"`
	LinesJSON      string    `gorm:"type:text" json:"lines_json"`
	AmountCents    int64     `json:"amount_cents"`
	RefundID       string    `gorm:"size:100" json:"refund_id"`
	ReturnTracking string    `gorm:"size:100" json:"return_tracking"`
	ReplacementSKU string    `gorm:"size:100" json:"replacement_sku"`
	ReplacementID  string    `gorm:"index;size:100" json:"replacement_id"`
	Condition      string    `gorm:"size:30" json:"condition"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type OrderGuard struct {
	OrderID string `gorm:"primaryKey;size:100"`
	Version int64
}
type Line struct {
	SKUID    string `json:"sku_id"`
	Quantity int64  `json:"quantity"`
}
