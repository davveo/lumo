package domain

import "time"

// Popup campaigns are private operational configuration, deliberately separate
// from public content documents. Only the delivery DTO is sent to shoppers.
type PopupCampaign struct {
	ID          string `gorm:"primaryKey;size:128"`
	Name        string `gorm:"size:120"`
	Status      string `gorm:"size:16;index:idx_content_popup_active,priority:1"`
	Priority    int    `gorm:"index:idx_content_popup_active,priority:2"`
	Payload     string `gorm:"type:mediumtext"`
	Version     int64
	Impressions int64
	Clicks      int64
	Dismissals  int64
	Claims      int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (PopupCampaign) TableName() string { return "content_popup_campaigns" }

type PopupSettings struct {
	ID            string    `gorm:"primaryKey;size:32" json:"-"`
	Enabled       bool      `json:"enabled"`
	MaxPerSession int       `json:"max_per_session"`
	MinGapSeconds int       `json:"min_gap_seconds"`
	CreatedAt     time.Time `json:"-"`
	UpdatedAt     time.Time `json:"-"`
}

func (PopupSettings) TableName() string { return "content_popup_settings" }

type PopupVisitor struct {
	ID           string `gorm:"primaryKey;size:64"`
	FirstSession string `gorm:"size:64"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (PopupVisitor) TableName() string { return "content_popup_visitors" }

// A subject row is the serialization point across all campaigns and sessions
// for a shopper. Different service replicas therefore share the same limits.
type PopupSubject struct {
	ID          string `gorm:"primaryKey;size:64"`
	LastShownAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (PopupSubject) TableName() string { return "content_popup_subjects" }

type PopupCounter struct {
	ID             string `gorm:"primaryKey;size:64"`
	Impressions    int
	Day            string `gorm:"size:10"`
	DayImpressions int
	LastShownAt    *time.Time
	LastClosedAt   *time.Time
	Claimed        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (PopupCounter) TableName() string { return "content_popup_counters" }

type PopupDelivery struct {
	ID         string `gorm:"primaryKey;size:128"`
	CampaignID string `gorm:"size:128;index"`
	Version    int64
	SubjectID  string    `gorm:"size:64;index:idx_content_popup_subject_expiry,priority:1"`
	SessionID  string    `gorm:"size:64"`
	ViewID     string    `gorm:"size:128"`
	ExpiresAt  time.Time `gorm:"index:idx_content_popup_subject_expiry,priority:2"`
	ShownAt    *time.Time
	ClickedAt  *time.Time
	ClosedAt   *time.Time
	ClaimedAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (PopupDelivery) TableName() string { return "content_popup_deliveries" }
