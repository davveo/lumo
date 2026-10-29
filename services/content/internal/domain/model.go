package domain

import "time"

// Record is this service's owned document aggregate. The relational keys and
// lifecycle are indexed; Payload contains the aggregate-specific attributes.
type Record struct {
	Version     int64      `gorm:"not null;default:1" json:"version"`
	PublishAt   *time.Time `json:"publish_at"`
	UnpublishAt *time.Time `json:"unpublish_at"`
	ID          string     `gorm:"primaryKey;size:128" json:"id"`
	UserID      string     `gorm:"size:128;index" json:"user_id,omitempty"`
	ParentID    string     `gorm:"size:128;index" json:"parent_id,omitempty"`
	Kind        string     `gorm:"size:32;index" json:"kind"`
	Status      string     `gorm:"size:32;index" json:"status"`
	Payload     string     `gorm:"type:mediumtext" json:"-"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (Record) TableName() string { return "content_records" }
