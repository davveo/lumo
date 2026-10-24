package domain

import "time"

type Cap struct {
	PaymentID string `gorm:"primaryKey;size:80"`
	Paid      int64
	Reserved  int64
	Completed int64
}
type Refund struct {
	ID          string    `gorm:"primaryKey;size:100" json:"id"`
	OrderID     string    `gorm:"index;size:80"       json:"order_id"`
	PaymentID   string    `gorm:"index;size:80"       json:"payment_id"`
	UserID      string    `gorm:"index;size:80"       json:"user_id"`
	AftersaleID string    `gorm:"size:80"             json:"aftersale_id"`
	Amount      int64     `                           json:"amount_cents"`
	Status      string    `gorm:"index;size:32"       json:"status"`
	Reason      string    `gorm:"size:240"            json:"reason"`
	Provider    string    `gorm:"size:32"             json:"provider"`
	CreatedAt   time.Time `                           json:"created_at"`
	UpdatedAt   time.Time `                           json:"updated_at"`
}
