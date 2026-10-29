package domain

import "time"

type Team struct {
	RuleID      string    `gorm:"size:120;index" json:"rule_id"`
	ActivityID  string    `gorm:"size:120;index" json:"activity_id"`
	RuleVersion int64     `json:"rule_version"`
	ID          string    `gorm:"primaryKey;size:120"   json:"id"`
	SKUID       string    `gorm:"column:sku_id;size:80" json:"sku_id"`
	CreatorID   string    `gorm:"size:80" json:"creator_id"`
	Required    int64     `json:"required"`
	Occupied    int64     `json:"occupied"`
	Paid        int64     `json:"paid"`
	State       string    `gorm:"size:24;index"         json:"status"`
	ExpiresAt   time.Time `gorm:"index" json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}
type Seat struct {
	ID        string    `gorm:"primaryKey;size:120" json:"order_id"`
	GroupID   string    `gorm:"size:120;index" json:"group_id"`
	UserID    string    `gorm:"size:80;index" json:"user_id"`
	State     string    `gorm:"size:24" json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Rule governs new teams; existing teams keep their immutable deadline and size.
type Rule struct {
	ID              string     `gorm:"primaryKey;size:120" json:"id"`
	Name            string     `gorm:"size:120" json:"name"`
	SKUID           string     `gorm:"column:sku_id;size:80;index" json:"sku_id"`
	ActivityID      string     `gorm:"size:120" json:"activity_id"`
	Required        int64      `json:"required"`
	DurationMinutes int64      `json:"duration_minutes"`
	Active          bool       `json:"active"`
	StartsAt        *time.Time `json:"starts_at"`
	EndsAt          *time.Time `json:"ends_at"`
	Version         int64      `gorm:"not null;default:1" json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
