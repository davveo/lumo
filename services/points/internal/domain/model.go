package domain

import "time"

type Account struct {
	UserID    string    `gorm:"primaryKey;size:80" json:"user_id"`
	Available int64     `json:"available"`
	Frozen    int64     `json:"frozen"`
	Debt      int64     `json:"debt"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Hold struct {
	ID        string    `gorm:"primaryKey;size:120" json:"id"`
	UserID    string    `gorm:"size:80;index" json:"user_id"`
	Amount    int64     `json:"amount"`
	Returned  int64     `json:"returned"`
	State     string    `gorm:"size:24" json:"state"`
	CreatedAt time.Time `json:"created_at"`
}
type Ledger struct {
	ID        string    `gorm:"primaryKey;size:160" json:"id"`
	UserID    string    `gorm:"size:80;index" json:"user_id"`
	OrderID   string    `gorm:"size:120;index" json:"order_id"`
	Amount    int64     `json:"amount"`
	Kind      string    `gorm:"size:24" json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}
type Reward struct {
	OrderID  string `gorm:"primaryKey;size:120" json:"order_id"`
	UserID   string `gorm:"size:80" json:"user_id"`
	Amount   int64  `json:"amount"`
	Reversed int64  `json:"reversed"`
}
