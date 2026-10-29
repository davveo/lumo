package domain

import "time"

// Subscription is a one-shot, server-checked product alert. SlotKey prevents
// duplicate active subscriptions for the same owner, SKU and alert kind.
type Subscription struct {
	ID           string     `gorm:"primaryKey;size:80" json:"id"`
	SlotKey      string     `gorm:"uniqueIndex;size:64" json:"-"`
	UserID       string     `gorm:"size:128;index" json:"user_id"`
	SKUID        string     `gorm:"size:128;index" json:"sku_id"`
	ProductName  string     `gorm:"size:240" json:"product_name"`
	Kind         string     `gorm:"size:24;index" json:"kind"`
	Status       string     `gorm:"size:24;index:idx_alert_due,priority:1" json:"status"`
	TargetCents  int64      `json:"target_cents"`
	PriceCents   int64      `json:"price_cents"`
	Generation   int64      `json:"generation"`
	ExpiresAt    time.Time  `json:"expires_at"`
	NextCheckAt  time.Time  `gorm:"index:idx_alert_due,priority:2" json:"next_check_at"`
	TriggeredAt  *time.Time `json:"triggered_at,omitempty"`
	CancelledBy  string     `gorm:"size:128" json:"-"`
	CancelReason string     `gorm:"size:240" json:"cancel_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (Subscription) TableName() string { return "engagement_subscriptions" }
