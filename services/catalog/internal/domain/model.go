package domain

import "time"

// Record is this service's owned document aggregate. The relational keys and
// lifecycle are indexed; Payload contains the aggregate-specific attributes.
// idx_catalog_page covers public listing filters/counts and timestamp ordering.
// Keep its existing MySQL name and ascending column order when migrating.
type Record struct {
	ID        string    `gorm:"primaryKey;size:128;index:idx_catalog_page,priority:5" json:"id"`
	UserID    string    `gorm:"size:128;index" json:"user_id,omitempty"`
	ParentID  string    `gorm:"size:128;index;index:idx_catalog_page,priority:2" json:"parent_id,omitempty"`
	Kind      string    `gorm:"size:32;index;index:idx_catalog_page,priority:1" json:"kind"`
	Status    string    `gorm:"size:32;index;index:idx_catalog_page,priority:3" json:"status"`
	Payload   string    `gorm:"type:text" json:"-"`
	CreatedAt time.Time `gorm:"index:idx_catalog_page,priority:4" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// MySQL owns this generated, indexed projection. The explicit repository
	// migration verifies its expression; GORM must never write or alter it.
	SearchCategoryV1 string `gorm:"column:search_category_v1;->;-:migration" json:"-"`
}

func (Record) TableName() string { return "catalog_records" }
