package domain

import "time"

type Gate struct {
	OrderID string `gorm:"primaryKey;size:80"`
	Frozen  bool
	Version int64
}
type Payment struct {
	ID        string     `gorm:"primaryKey;size:80" json:"id"`
	OrderID   string     `gorm:"uniqueIndex;size:80" json:"order_id"`
	UserID    string     `gorm:"index;size:80" json:"user_id"`
	Amount    int64      `json:"amount_cents"`
	Status    string     `gorm:"index;size:32" json:"status"`
	Provider  string     `gorm:"size:40" json:"provider"`
	Receipt   string     `gorm:"size:100" json:"receipt"`
	PaidAt    *time.Time `json:"paid_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
