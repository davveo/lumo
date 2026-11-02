package domain

import "time"

type BehaviorFact struct {
	ID         string    `gorm:"primaryKey;size:128"`
	UserID     string    `gorm:"size:128;index"`
	Type       string    `gorm:"size:24;index"`
	OccurredAt time.Time `gorm:"index"`
}
type TransactionFact struct {
	ID          string `gorm:"primaryKey;size:128"`
	OrderID     string `gorm:"size:128;index"`
	UserID      string `gorm:"size:128;index"`
	Kind        string `gorm:"size:12;index"`
	AmountCents int64
	Items       string    `gorm:"type:longtext"`
	OccurredAt  time.Time `gorm:"index"`
}

type MetricsHistory struct {
	ID              string `gorm:"primaryKey;size:20"`
	PaymentComplete bool
	RefundComplete  bool
	CompletedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
