package domain

import "time"

type Run struct {
	ID        string    `gorm:"primaryKey;size:80" json:"id"`
	Status    string    `gorm:"size:32" json:"status"`
	Details   string    `gorm:"type:longtext" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}
