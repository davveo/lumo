package domain

import "time"

type ExportJob struct {
	ID          string    `gorm:"primaryKey;size:128" json:"id"`
	OwnerID     string    `gorm:"size:128;index" json:"owner_id"`
	Source      string    `gorm:"size:80" json:"source"`
	RecordID    string    `gorm:"size:128" json:"record_id,omitempty"`
	FiltersJSON string    `gorm:"type:text" json:"filters"`
	Status      string    `gorm:"size:24;index" json:"status"`
	Processed   int64     `json:"processed"`
	Total       int64     `json:"total"`
	Bytes       int64     `json:"bytes"`
	Error       string    `gorm:"size:500" json:"error,omitempty"`
	FileID      string    `gorm:"size:128" json:"-"`
	Claim       string    `gorm:"size:80" json:"-"`
	LeaseUntil  time.Time `json:"-"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ExportJob) TableName() string { return "helpdesk_exports" }

type ExportChunk struct {
	JobID    string `gorm:"primaryKey;size:128"`
	Page     int    `gorm:"primaryKey"`
	RowsJSON string `gorm:"type:longtext"`
}

func (ExportChunk) TableName() string { return "helpdesk_export_chunks" }

type ExportRow struct {
	JobID string `gorm:"primaryKey;size:128"`
	RowID string `gorm:"primaryKey;size:200"`
}

func (ExportRow) TableName() string { return "helpdesk_export_rows" }
