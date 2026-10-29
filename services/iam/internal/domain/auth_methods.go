package domain

import "time"

type Identity struct {
	ID        string `gorm:"primaryKey;size:64"`
	Provider  string `gorm:"size:20"`
	Subject   string `gorm:"size:160" json:"-"`
	AccountID string `gorm:"size:128;index"`
}
type AuthChallenge struct {
	ID         string `gorm:"primaryKey;size:64" json:"-"`
	Provider   string `gorm:"size:20"`
	Subject    string `gorm:"size:160" json:"-"`
	Digest     string `gorm:"size:64" json:"-"`
	Binding    string `gorm:"size:64" json:"-"`
	Delivered  bool
	Attempts   int64
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}
type AuthRate struct {
	ID       string `gorm:"primaryKey;size:64"`
	LastSent time.Time
	Day      string `gorm:"size:10"`
	Count    int64
}
