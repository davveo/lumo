package domain

import "time"

type StaffChange struct {
	ID        string    `gorm:"primaryKey;size:128" json:"id"`
	UserID    string    `gorm:"size:128;index" json:"user_id"`
	ActorID   string    `gorm:"size:128" json:"actor_id"`
	OldRole   string    `gorm:"size:32" json:"old_role"`
	NewRole   string    `gorm:"size:32" json:"new_role"`
	Active    bool      `json:"active"`
	Reason    string    `gorm:"size:240" json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
