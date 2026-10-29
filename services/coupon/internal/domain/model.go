package domain

import (
	"github.com/davveo/lumo/internal/couponrules"
	"time"
)

type Coupon struct {
	couponrules.Rule `gorm:"embedded"`
	ID               string    `gorm:"primaryKey;size:120" json:"id"`
	TemplateID       string    `gorm:"size:120;index"      json:"template_id,omitempty"`
	UserID           string    `gorm:"size:80;index" json:"user_id"`
	Amount           int64     `                           json:"amount_cents"`
	Minimum          int64     `json:"min_cents"`
	State            string    `gorm:"size:24" json:"status"`
	ExpiresAt        time.Time `json:"expires_at"`
}
type Template struct {
	couponrules.Rule `gorm:"embedded"`
	Version          int64      `gorm:"not null;default:1" json:"version"`
	ID               string     `gorm:"primaryKey;size:120" json:"id"`
	Name             string     `gorm:"size:120" json:"name"`
	Amount           int64      `                           json:"amount_cents"`
	Minimum          int64      `json:"min_cents"`
	ValidDays        int64      `json:"valid_days"`
	Remaining        int64      `json:"remaining"`
	Active           bool       `json:"active"`
	StartsAt         *time.Time `json:"starts_at"`
	MinPoints        int64      `json:"min_points"`
	MinGrowth        int64      `json:"min_growth"`
	ExpiresAt        time.Time  `json:"expires_at"`
	CreatedAt        time.Time  `json:"created_at"`
}
type Claim struct {
	ID         string    `gorm:"primaryKey;size:80"                       json:"id"`
	UserID     string    `gorm:"size:80;uniqueIndex:claim_user_template"  json:"user_id"`
	TemplateID string    `gorm:"size:120;uniqueIndex:claim_user_template" json:"template_id"`
	CouponID   string    `gorm:"size:120;uniqueIndex"                     json:"coupon_id"`
	CreatedAt  time.Time `json:"created_at"`
}
type Hold struct {
	ID        string    `gorm:"primaryKey;size:120" json:"id"`
	UserID    string    `gorm:"size:80;index" json:"user_id"`
	CouponID  string    `gorm:"size:120" json:"coupon_id"`
	Amount    int64     `json:"discount_cents"`
	State     string    `gorm:"size:24" json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type Ledger struct {
	ID        string    `gorm:"primaryKey;size:160" json:"id"`
	OrderID   string    `gorm:"size:120" json:"order_id"`
	CouponID  string    `gorm:"size:120" json:"coupon_id"`
	Kind      string    `gorm:"size:24" json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}
