package domain

import "time"

type Saga struct {
	ID          string    `gorm:"primaryKey;size:80" json:"order_id"`
	UserID      string    `gorm:"index;size:80" json:"user_id"`
	RequestKey  string    `gorm:"uniqueIndex;size:64" json:"-"`
	RequestHash string    `gorm:"size:64" json:"-"`
	QuoteID     string    `gorm:"size:80" json:"quote_id"`
	Quote       string    `gorm:"type:longtext" json:"-"`
	Source      string    `gorm:"size:16" json:"source"`
	Note        string    `gorm:"size:1000" json:"note,omitempty"`
	Address     string    `gorm:"type:longtext" json:"-"`
	State       string    `gorm:"index;size:40" json:"status"`
	Steps       string    `gorm:"type:longtext" json:"-"`
	LastError   string    `gorm:"size:500" json:"last_error,omitempty"`
	LeaseOwner  string    `gorm:"size:80" json:"-"`
	LeaseUntil  time.Time `gorm:"index" json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
