package domain

import "time"

type CustomerCard struct {
	UserID    string    `gorm:"primaryKey;size:128" json:"user_id"`
	Tags      string    `gorm:"type:text" json:"-"`
	Note      string    `gorm:"type:text" json:"note"`
	Version   int64     `json:"version"`
	UpdatedBy string    `gorm:"size:128" json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}
type CustomerSegment struct {
	ID           string    `gorm:"primaryKey;size:128" json:"id"`
	Name         string    `gorm:"size:100" json:"name"`
	MinOrders    int64     `json:"min_orders"`
	MinNetCents  int64     `json:"min_net_cents"`
	InactiveDays int64     `json:"inactive_days"`
	Version      int64     `json:"version"`
	UpdatedBy    string    `gorm:"size:128" json:"updated_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
type CustomerChange struct {
	ID         string    `gorm:"primaryKey;size:128" json:"id"`
	CustomerID string    `gorm:"size:128;index" json:"customer_id"`
	ActorID    string    `gorm:"size:128" json:"actor_id"`
	Detail     string    `gorm:"type:text" json:"-"`
	CreatedAt  time.Time `json:"created_at"`
}
