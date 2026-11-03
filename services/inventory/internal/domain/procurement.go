package domain

import "time"

type PurchaseOrder struct {
	SupplierID      string     `gorm:"size:128;index" json:"supplier_id"`
	LeadDays        int64      `json:"lead_days"`
	ID              string     `gorm:"primaryKey;size:128" json:"id"`
	Supplier        string     `gorm:"size:120;index" json:"supplier"`
	SupplierContact string     `gorm:"size:160" json:"supplier_contact"`
	Note            string     `gorm:"size:500" json:"note"`
	Status          string     `gorm:"size:32;index" json:"status"`
	Version         int64      `json:"version"`
	ActorID         string     `gorm:"size:128" json:"actor_id"`
	ApprovedAt      *time.Time `json:"approved_at"`
	CancelledBy     string     `gorm:"size:128" json:"cancelled_by"`
	CancelReason    string     `gorm:"size:240" json:"cancel_reason"`
	ApprovedBy      string     `gorm:"size:128" json:"approved_by"`
	Fingerprint     string     `gorm:"size:64" json:"-"`
	Lines           string     `gorm:"type:longtext" json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
type PurchaseLine struct {
	SKUID         string `json:"sku_id"`
	Quantity      int64  `json:"quantity"`
	UnitCostCents int64  `json:"unit_cost_cents"`
	Received      int64  `json:"received"`
	Damaged       int64  `json:"damaged"`
}
type PurchaseReceipt struct {
	ID          string    `gorm:"primaryKey;size:128" json:"id"`
	PurchaseID  string    `gorm:"size:128;index" json:"purchase_id"`
	ActorID     string    `gorm:"size:128" json:"actor_id"`
	Lines       string    `gorm:"type:longtext" json:"-"`
	Fingerprint string    `gorm:"size:64" json:"-"`
	Note        string    `gorm:"size:500" json:"note"`
	CreatedAt   time.Time `json:"created_at"`
}
type PurchaseReceiveLine struct {
	SKUID    string `json:"sku_id"`
	Quantity int64  `json:"quantity"`
	Damaged  int64  `json:"damaged"`
}
