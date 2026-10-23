package domain

import "time"

type Order struct {
	ID                 string    `gorm:"primaryKey;size:80" json:"id"`
	UserID             string    `gorm:"index;size:80" json:"user_id"`
	Status             string    `gorm:"index;size:40" json:"status"`
	Version            int64     `json:"version"`
	Payable            int64     `json:"payable_cents"`
	Refunded           int64     `json:"refunded_total_cents"`
	Quote              string    `gorm:"type:longtext" json:"-"`
	Source             string    `gorm:"size:16" json:"source"`
	Note               string    `gorm:"size:1000" json:"note,omitempty"`
	Address            string    `gorm:"type:longtext" json:"-"`
	PaymentID          string    `gorm:"size:80" json:"payment_id"`
	GroupID            string    `gorm:"index;size:80" json:"group_id"`
	GroupFormed        bool      `json:"group_formed"`
	GroupFailed        bool      `json:"group_failed"`
	ResourcesCommitted bool      `json:"resources_committed"`
	ExpiresAt          time.Time `gorm:"index" json:"expires_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
type RefundReceipt struct {
	ID        string `gorm:"primaryKey;size:80"`
	OrderID   string `gorm:"index;size:80"`
	Amount    int64
	CreatedAt time.Time
}
