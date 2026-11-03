package domain

import "time"

type Supplier struct {
	ID        string    `gorm:"primaryKey;size:128" json:"id"`
	Name      string    `gorm:"size:120;index" json:"name"`
	Contact   string    `gorm:"size:160" json:"contact"`
	LeadDays  int64     `json:"lead_days"`
	Version   int64     `json:"version"`
	UpdatedBy string    `gorm:"size:128" json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
