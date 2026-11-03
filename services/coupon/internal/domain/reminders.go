package domain

import "time"

type ExpiryPreference struct {
	UserID       string     `gorm:"primaryKey;size:100" json:"user_id"`
	Enabled      bool       `json:"enabled"`
	LastQueuedAt *time.Time `json:"last_queued_at,omitempty"`
}

func (ExpiryPreference) TableName() string { return "coupon_expiry_preferences" }

// RewardReceipt binds a paid points redemption to exactly one coupon issuance.
type RewardReceipt struct {
	ID         string    `gorm:"primaryKey;size:100" json:"id"`
	UserID     string    `gorm:"size:80" json:"user_id"`
	TemplateID string    `gorm:"size:120" json:"template_id"`
	CouponID   string    `gorm:"size:120" json:"coupon_id"`
	CreatedAt  time.Time `json:"created_at"`
}
