package domain

import "time"

// Record is this service's owned document aggregate. The relational keys and
// lifecycle are indexed; Payload contains the aggregate-specific attributes.
type Record struct {
	ID        string    `gorm:"primaryKey;size:128" json:"id"`
	UserID    string    `gorm:"size:128;index" json:"user_id,omitempty"`
	ParentID  string    `gorm:"size:128;index" json:"parent_id,omitempty"`
	Kind      string    `gorm:"size:32;index" json:"kind"`
	Status    string    `gorm:"size:32;index" json:"status"`
	Payload   string    `gorm:"type:text" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Record) TableName() string { return "engagement_records" }

// CompareOwner serializes bounded-list additions across service replicas. It is
// not an engagement record and cannot appear in favorites/history or counters.
type CompareOwner struct {
	UserID string `gorm:"primaryKey;size:128"`
}

func (CompareOwner) TableName() string { return "engagement_compare_owners" }
