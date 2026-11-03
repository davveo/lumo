package domain

import "time"

type WorkAssignment struct {
	ID           string    `gorm:"primaryKey;size:180" json:"id"`
	Service      string    `gorm:"size:32;index" json:"service"`
	ResourceID   string    `gorm:"size:128" json:"resource_id"`
	OrderID      string    `gorm:"size:100;index" json:"order_id"`
	AssigneeID   string    `gorm:"size:100;index" json:"assignee_id"`
	AssigneeName string    `gorm:"size:100" json:"assignee_name"`
	ActorID      string    `gorm:"size:100" json:"actor_id"`
	Version      int64     `json:"version"`
	RequestKey   string    `gorm:"size:160" json:"-"`
	UpdatedAt    time.Time `json:"updated_at"`
}
type WorkChange struct {
	ID        string    `gorm:"primaryKey;size:180" json:"id"`
	WorkID    string    `gorm:"size:180;index" json:"work_id"`
	ActorID   string    `gorm:"size:100" json:"actor_id"`
	Before    string    `gorm:"size:100" json:"before"`
	After     string    `gorm:"size:100" json:"after"`
	Reason    string    `gorm:"size:240" json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
type WorkView struct {
	ID          string    `gorm:"primaryKey;size:100" json:"id"`
	UserID      string    `gorm:"size:100;index" json:"user_id"`
	Name        string    `gorm:"size:100" json:"name"`
	FiltersJSON string    `gorm:"type:text" json:"filters_json"`
	Version     int64     `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type WorkViewGuard struct {
	UserID string `gorm:"primaryKey;size:100"`
}
