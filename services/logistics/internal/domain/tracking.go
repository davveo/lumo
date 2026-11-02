package domain

import "time"

type TrackingEvent struct {
	ID          string    `gorm:"primaryKey;size:128" json:"id"`
	LogisticsID string    `gorm:"size:100;index" json:"logistics_id"`
	TrackingID  string    `gorm:"size:100" json:"tracking_id"`
	Status      string    `gorm:"size:32" json:"status"`
	Location    string    `gorm:"size:160" json:"location"`
	Description string    `gorm:"size:500" json:"description"`
	Source      string    `gorm:"size:24" json:"source"`
	ActorID     string    `gorm:"size:128" json:"actor_id,omitempty"`
	Fingerprint string    `gorm:"size:64" json:"-"`
	OccurredAt  time.Time `gorm:"index" json:"occurred_at"`
	CreatedAt   time.Time `json:"created_at"`
}
type DeliveryException struct {
	ID          string     `gorm:"primaryKey;size:128" json:"id"`
	LogisticsID string     `gorm:"size:100;index" json:"logistics_id"`
	TrackingID  string     `gorm:"size:100" json:"tracking_id"`
	UserID      string     `gorm:"size:128" json:"user_id"`
	Kind        string     `gorm:"size:32;index" json:"kind"`
	Status      string     `gorm:"size:16;index" json:"status"`
	Details     string     `gorm:"size:500" json:"details"`
	Resolution  string     `gorm:"size:500" json:"resolution"`
	ResolvedBy  string     `gorm:"size:128" json:"resolved_by"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
