package domain

import "time"

type Record struct {
	ID            string    `gorm:"primaryKey;size:100" json:"id"`
	UserID        string    `gorm:"index;size:80" json:"user_id"`
	OrderID       string    `gorm:"uniqueIndex;size:100" json:"order_id"`
	Kind          string    `gorm:"size:30" json:"kind"`
	Title         string    `gorm:"size:200" json:"title"`
	TaxID         string    `gorm:"size:40" json:"tax_id"`
	Status        string    `gorm:"index;size:40" json:"status"`
	AmountCents   int64     `json:"amount_cents"`
	IssuedCents   int64     `json:"issued_cents"`
	Number        string    `gorm:"size:100" json:"number"`
	RefundedCents int64     `json:"refunded_cents"`
	Provider      string    `gorm:"size:40" json:"provider"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
type Reversal struct {
	ID             string    `gorm:"primaryKey;size:150" json:"id"`
	InvoiceID      string    `gorm:"index;size:100" json:"invoice_id"`
	RefundID       string    `gorm:"uniqueIndex;size:100" json:"refund_id"`
	OriginalNumber string    `gorm:"size:100" json:"original_number"`
	AmountCents    int64     `json:"amount_cents"`
	Status         string    `gorm:"size:40" json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}
