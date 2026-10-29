package domain

import "time"

type Quote struct {
	ID        string    `gorm:"primaryKey;size:80" json:"quote_id"`
	UserID    string    `gorm:"index;size:80" json:"user_id"`
	Payload   string    `gorm:"type:longtext" json:"-"`
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}
type Item struct {
	Category  string `json:"category"`
	ProductID string `json:"product_id"`
	SKUID     string `json:"sku_id"`
	Quantity  int64  `json:"quantity"`
	Name      string `json:"name"`
	UnitPrice int64  `json:"unit_price_cents"`
	Amount    int64  `json:"amount_cents"`
}
