package domain

import (
	"context"
	"time"
)

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

func (Record) TableName() string { return "media_records" }

type StoredObject struct {
	Size        int64
	ContentType string
	ETag        string
}
type SignedUpload struct {
	URL    string
	Method string
	Fields map[string]string
}

// ObjectStore separates S3 operations from domain rules and allows isolated
// authorization/validation tests without pretending a remote upload succeeded.
type ObjectStore interface {
	EnsureBucket(context.Context) error
	SignUpload(context.Context, string, string, int64, time.Duration) (SignedUpload, error)
	Stat(context.Context, string) (StoredObject, error)
	SignDownload(context.Context, string, time.Duration) (string, error)
}
