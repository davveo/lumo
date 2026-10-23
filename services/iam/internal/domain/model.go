package domain

import "time"

type Account struct {
	ID           string    `gorm:"primaryKey;size:128" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:128" json:"username"`
	PasswordHash string    `gorm:"size:128" json:"-"`
	Role         string    `gorm:"size:32" json:"role"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Account) TableName() string { return "iam_accounts" }

type Session struct {
	Digest    string     `gorm:"primaryKey;size:64" json:"-"`
	UserID    string     `gorm:"size:128;index" json:"user_id"`
	ExpiresAt time.Time  `gorm:"index" json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (Session) TableName() string { return "iam_sessions" }
