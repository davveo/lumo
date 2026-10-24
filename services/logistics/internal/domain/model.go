package domain

import "time"

type Record struct {
	ID         string    `gorm:"primaryKey;size:100"  json:"id"`
	UserID     string    `gorm:"index;size:80"        json:"user_id"`
	OrderID    string    `gorm:"index;size:100"       json:"order_id"`
	Kind       string    `gorm:"size:30"              json:"kind"`
	Status     string    `gorm:"index;size:40"        json:"status"`
	TrackingID string    `gorm:"uniqueIndex;size:100" json:"tracking_id"`
	Provider   string    `gorm:"size:40"              json:"provider"`
	Version    int64     `                            json:"version"`
	CreatedAt  time.Time `                            json:"created_at"`
	UpdatedAt  time.Time `                            json:"updated_at"`
}
