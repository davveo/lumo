package domain

import "time"

type RedeemOffer struct {
	ID          string    `gorm:"primaryKey;size:100" json:"id"`
	Name        string    `gorm:"size:120" json:"name"`
	Description string    `gorm:"size:500" json:"description"`
	Kind        string    `gorm:"size:20" json:"kind"`
	ReferenceID string    `gorm:"size:120" json:"reference_id"`
	Cost        int64     `json:"cost"`
	TotalQuota  int64     `json:"total_quota"`
	Used        int64     `json:"used"`
	MaxPerUser  int64     `json:"max_per_user"`
	Active      bool      `json:"active"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	Version     int64     `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (RedeemOffer) TableName() string { return "points_redeem_offers" }

type Redemption struct {
	ID            string     `gorm:"primaryKey;size:100" json:"id"`
	RequestKey    string     `gorm:"uniqueIndex;size:64" json:"-"`
	RequestHash   string     `gorm:"size:64" json:"-"`
	UserID        string     `gorm:"index;size:80" json:"user_id"`
	OfferID       string     `gorm:"index;size:100" json:"offer_id"`
	Name          string     `gorm:"size:120" json:"name"`
	Kind          string     `gorm:"size:20" json:"kind"`
	ReferenceID   string     `gorm:"size:120" json:"reference_id"`
	Cost          int64      `json:"cost"`
	AddressJSON   string     `gorm:"type:text" json:"-"`
	State         string     `gorm:"index;size:24" json:"state"`
	CouponID      string     `gorm:"size:120" json:"coupon_id,omitempty"`
	FulfillmentID string     `gorm:"size:100" json:"fulfillment_id,omitempty"`
	TrackingID    string     `gorm:"size:100" json:"tracking_id,omitempty"`
	LastError     string     `gorm:"size:500" json:"last_error,omitempty"`
	LeaseToken    string     `gorm:"size:100" json:"-"`
	LeaseUntil    *time.Time `json:"-"`
	NextTry       time.Time  `gorm:"index" json:"-"`
	Attempts      int64      `json:"attempts"`
	Version       int64      `json:"version"`
	CreatedAt     time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (Redemption) TableName() string { return "points_redemptions" }
