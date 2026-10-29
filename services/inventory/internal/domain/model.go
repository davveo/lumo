package domain

import "time"

type Stock struct {
	Threshold int64     `gorm:"not null;default:10" json:"threshold"`
	SKUID     string    `gorm:"column:sku_id;primaryKey;size:80" json:"sku_id"`
	OnHand    int64     `                                        json:"on_hand"`
	Reserved  int64     `                                        json:"reserved"`
	Allocated int64     `                                        json:"allocated"`
	UpdatedAt time.Time `                                        json:"updated_at"`
}
type Line struct {
	SKUID    string `json:"sku_id"`
	Quantity int64  `json:"quantity"`
}
type Reservation struct {
	ID        string    `gorm:"primaryKey;size:120" json:"id"`
	UserID    string    `gorm:"size:80;index"       json:"user_id"`
	OrderID   string    `gorm:"size:120;index"      json:"order_id"`
	State     string    `gorm:"size:24"             json:"state"`
	LinesJSON string    `gorm:"type:text"           json:"-"`
	CreatedAt time.Time `                           json:"created_at"`
	UpdatedAt time.Time `                           json:"updated_at"`
}
type Ledger struct {
	ID          string    `gorm:"primaryKey;size:160"         json:"id"`
	ReferenceID string    `gorm:"size:120;index"              json:"reference_id"`
	UserID      string    `gorm:"size:80;index"               json:"user_id"`
	SKUID       string    `gorm:"column:sku_id;size:80;index" json:"sku_id"`
	Quantity    int64     `                                   json:"quantity"`
	Kind        string    `gorm:"size:30"                     json:"kind"`
	CreatedAt   time.Time `                                   json:"created_at"`
}

type Adjustment struct {
	ID        string    `gorm:"primaryKey;size:160;index:idx_inventory_adjustment_history,priority:3" json:"id"`
	SKUID     string    `gorm:"column:sku_id;size:80;index:idx_inventory_adjustment_history,priority:1" json:"sku_id"`
	ActorID   string    `gorm:"size:128" json:"actor_id"`
	Before    int64     `json:"before"`
	After     int64     `json:"after"`
	Reason    string    `gorm:"size:240" json:"reason"`
	CreatedAt time.Time `gorm:"index:idx_inventory_adjustment_history,priority:2" json:"created_at"`
}
