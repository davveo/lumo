package domain

import "time"

type JourneyEvent struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	SessionKey  string    `gorm:"size:64;index" json:"-"`
	UserID      string    `gorm:"size:128;index" json:"-"`
	Type        string    `gorm:"size:24;index" json:"type"`
	SKU         string    `gorm:"size:150;index" json:"sku_id"`
	Query       string    `gorm:"size:400" json:"query"`
	Source      string    `gorm:"size:24;index" json:"source"`
	ResultCount int64     `json:"result_count"`
	OccurredAt  time.Time `gorm:"index" json:"occurred_at"`
}

func (JourneyEvent) TableName() string { return "analytics_journey_events" }

type JourneyLimit struct {
	ID        string `gorm:"primaryKey;size:100"`
	Count     int64
	UpdatedAt time.Time
}

func (JourneyLimit) TableName() string { return "analytics_journey_limits" }
