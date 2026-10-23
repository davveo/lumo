package domain

import "time"

type Session struct {
	ID         string `gorm:"primaryKey;size:120" json:"id"`
	SKUID      string `gorm:"column:sku_id;size:80" json:"sku_id"`
	Name       string `gorm:"size:120" json:"name"`
	ActivityID string `gorm:"size:120" json:"activity_id"`
	Active     bool   `json:"active"`
}
type Request struct {
	ID          string    `gorm:"primaryKey;size:120" json:"id"`
	UserID      string    `gorm:"size:80;index;uniqueIndex:user_key" json:"user_id"`
	Key         string    `gorm:"size:120;uniqueIndex:user_key" json:"-"`
	QuoteID     string    `gorm:"size:120" json:"quote_id"`
	PayloadJSON string    `gorm:"type:text" json:"-"`
	State       string    `gorm:"size:24;index" json:"status"`
	OrderID     string    `gorm:"size:120" json:"order_id"`
	Error       string    `gorm:"size:200" json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
