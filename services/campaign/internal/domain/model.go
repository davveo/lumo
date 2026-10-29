package domain

import "time"

type Activity struct {
	Version     int64  `gorm:"not null;default:1" json:"version"`
	ID          string `gorm:"primaryKey;size:120" json:"id"`
	Kind        string `gorm:"size:32" json:"kind"`
	Name        string `gorm:"size:120" json:"name"`
	Description string `gorm:"size:255" json:"description"`
	Threshold   int64  `json:"threshold_cents"`
	Discount    int64  `json:"discount_cents"`
	PayRateBPS  int64  `json:"pay_rate_bps"`
	MinQuantity int64  `json:"min_quantity"`
	Quota       int64  `json:"quota"`
	Held        int64  `json:"held"`
	Used        int64  `json:"used"`
	Active      bool   `json:"active"`
}
type Hold struct {
	ID         string    `gorm:"primaryKey;size:120" json:"id"`
	UserID     string    `gorm:"size:80;index" json:"user_id"`
	ActivityID string    `gorm:"size:120;index" json:"activity_id"`
	Quantity   int64     `json:"quantity"`
	State      string    `gorm:"size:24" json:"state"`
	CreatedAt  time.Time `json:"created_at"`
}
