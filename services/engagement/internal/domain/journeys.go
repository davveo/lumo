package domain

import "time"

type JourneyRule struct {
	ID           string    `gorm:"primaryKey;size:128" json:"id"`
	Name         string    `gorm:"size:240" json:"name"`
	Kind         string    `gorm:"size:24;index" json:"kind"`
	Enabled      bool      `json:"enabled"`
	DelayMinutes int64     `json:"delay_minutes"`
	CycleDays    int64     `json:"cycle_days"`
	GapHours     int64     `json:"gap_hours"`
	SKUID        string    `gorm:"size:128" json:"sku_id,omitempty"`
	SegmentID    string    `gorm:"size:128" json:"segment_id,omitempty"`
	Version      int64     `json:"version"`
	ActorID      string    `gorm:"size:128" json:"actor_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (JourneyRule) TableName() string { return "engagement_journey_rules" }

type Journey struct {
	ID               string     `gorm:"primaryKey;size:128" json:"id"`
	SlotKey          string     `gorm:"size:64;uniqueIndex" json:"-"`
	UserID           string     `gorm:"size:128;index" json:"user_id"`
	RuleID           string     `gorm:"size:128;index" json:"rule_id"`
	Kind             string     `gorm:"size:24" json:"kind"`
	SourceID         string     `gorm:"size:128" json:"source_id"`
	SKUID            string     `gorm:"size:128" json:"sku_id,omitempty"`
	SKUsJSON         string     `gorm:"type:text" json:"-"`
	Status           string     `gorm:"size:24;index" json:"status"`
	Generation       int64      `json:"generation"`
	SourceAt         time.Time  `json:"source_at"`
	DueAt            time.Time  `gorm:"index" json:"due_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	QueuedAt         *time.Time `json:"queued_at,omitempty"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	ClickedAt        *time.Time `json:"clicked_at,omitempty"`
	RecoveredAt      *time.Time `json:"recovered_at,omitempty"`
	RecoveredOrderID string     `gorm:"size:128" json:"recovered_order_id,omitempty"`
	Reason           string     `gorm:"size:500" json:"reason,omitempty"`
	Claim            string     `gorm:"size:80" json:"-"`
	LeaseUntil       time.Time  `json:"-"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (Journey) TableName() string { return "engagement_journeys" }

type JourneyOwner struct {
	UserID       string `gorm:"primaryKey;size:128"`
	LastPaidAt   time.Time
	LastQueuedAt time.Time
}

func (JourneyOwner) TableName() string { return "engagement_journey_owners" }

type JourneyReceipt struct {
	ID        string `gorm:"primaryKey;size:128"`
	CreatedAt time.Time
}

func (JourneyReceipt) TableName() string { return "engagement_journey_receipts" }
