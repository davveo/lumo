package domain

import "time"

type Run struct {
	Reason          string    `gorm:"size:240" json:"reason"`
	From            string    `gorm:"size:10" json:"from"`
	To              string    `gorm:"size:10" json:"to"`
	ActorID         string    `gorm:"size:128" json:"actor_id"`
	Offset          int64     `json:"offset"`
	Total           int64     `json:"total"`
	Checked         int64     `json:"checked"`
	DifferenceCount int64     `json:"difference_count"`
	Error           string    `gorm:"size:255" json:"error,omitempty"`
	LeaseUntil      time.Time `json:"-"`
	Claim           string    `gorm:"size:80" json:"-"`
	UpdatedAt       time.Time `json:"updated_at"`

	ID        string    `gorm:"primaryKey;size:80" json:"id"`
	Status    string    `gorm:"size:32"            json:"status"`
	Details   string    `gorm:"type:longtext"      json:"-"`
	CreatedAt time.Time `                          json:"created_at"`
}

type Difference struct {
	ID        string    `gorm:"primaryKey;size:160" json:"id"`
	RunID     string    `gorm:"size:80;index" json:"run_id"`
	PaymentID string    `gorm:"size:120" json:"payment_id"`
	OrderID   string    `gorm:"size:120" json:"order_id"`
	Kind      string    `gorm:"size:80" json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}
type Followup struct {
	ID        string    `gorm:"primaryKey;size:160" json:"id"`
	RunID     string    `gorm:"size:80;index" json:"run_id"`
	ActorID   string    `gorm:"size:128" json:"actor_id"`
	Body      string    `gorm:"size:2000" json:"body"`
	CreatedAt time.Time `json:"created_at"`
}
