package domain

import "time"

type ProductDraft struct {
	Name        string    `gorm:"size:300" json:"name"`
	ProductID   string    `gorm:"primaryKey;size:128" json:"product_id"`
	Status      string    `gorm:"size:24" json:"status"`
	Version     int64     `json:"version"`
	BaseVersion int64     `json:"base_version"`
	DataJSON    string    `gorm:"type:longtext" json:"-"`
	EditorJSON  string    `gorm:"type:longtext" json:"-"`
	ActorID     string    `gorm:"size:128" json:"actor_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ProductDraft) TableName() string { return "catalog_product_drafts" }

type ProductRevision struct {
	ProductID string    `gorm:"primaryKey;size:128" json:"product_id"`
	Version   int64     `gorm:"primaryKey" json:"version"`
	RowsJSON  string    `gorm:"type:longtext" json:"-"`
	ActorID   string    `gorm:"size:128" json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (ProductRevision) TableName() string { return "catalog_product_revisions" }

type Publication struct {
	ID          string    `gorm:"primaryKey;size:128" json:"id"`
	ProductID   string    `gorm:"size:128;index" json:"product_id"`
	ActorID     string    `gorm:"size:128" json:"actor_id"`
	DependsOnID string    `gorm:"size:128" json:"depends_on_id,omitempty"`
	Mode        string    `gorm:"size:16" json:"mode"`
	RowsJSON    string    `gorm:"type:longtext" json:"-"`
	BaseVersion int64     `json:"base_version"`
	Status      string    `gorm:"size:24;index" json:"status"`
	Error       string    `gorm:"size:500" json:"error,omitempty"`
	RunAt       time.Time `gorm:"index" json:"run_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Publication) TableName() string { return "catalog_publications" }
