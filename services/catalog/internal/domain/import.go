package domain

import "time"

type ImportJob struct {
	ID        string    `gorm:"primaryKey;size:128" json:"id"`
	ActorID   string    `gorm:"size:128;index" json:"actor_id"`
	Status    string    `gorm:"size:32;index" json:"status"`
	Mode      string    `gorm:"size:16" json:"mode"`
	Total     int       `json:"total"`
	Processed int       `json:"processed"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Rows      string    `gorm:"type:longtext" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type ImportRow struct {
	Line    int       `json:"line"`
	ID      string    `json:"id"`
	Exists  bool      `json:"exists"`
	Version time.Time `json:"version"`
	Records []Record  `json:"records,omitempty"`
	Error   string    `json:"error,omitempty"`
	Status  string    `json:"status"`
}
