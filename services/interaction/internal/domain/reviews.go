package domain

import "time"

type ReviewFollowup struct {
	ReviewID    string    `gorm:"primaryKey;size:128" json:"review_id"`
	Body        string    `gorm:"type:text" json:"body"`
	PhotosJSON  string    `gorm:"type:text" json:"-"`
	Status      string    `gorm:"size:32;index" json:"status"`
	Reason      string    `gorm:"size:1500" json:"reason,omitempty"`
	Version     int64     `json:"version"`
	ModeratedBy string    `gorm:"size:128" json:"moderated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ReviewFollowup) TableName() string { return "interaction_review_followups" }
