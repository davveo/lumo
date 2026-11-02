package domain

import "time"

// SecurityChallenge is single-use and bound to both an account and a purpose.
type SecurityChallenge struct {
	ID         string `gorm:"primaryKey;size:64" json:"-"`
	AccountID  string `gorm:"size:128;index" json:"-"`
	Purpose    string `gorm:"size:24"`
	Channel    string `gorm:"size:12"`
	Subject    string `gorm:"size:160" json:"-"`
	Digest     string `gorm:"size:64" json:"-"`
	Attempts   int
	Delivered  bool
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}
