package repository

import (
	"context"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// These counters are in IAM's database so replicas cannot bypass limits. Keys
// contain purpose-separated digests, never usernames, IPs, or credentials.
type PasswordRate struct {
	ID            string    `gorm:"primaryKey;size:64"`
	WindowStarted time.Time `gorm:"index"`
	Attempts      int
	Failures      int
	BlockedUntil  *time.Time
	UpdatedAt     time.Time
}

func (PasswordRate) TableName() string { return "iam_password_rates" }

func passwordRateID(username string) string { return authKey("password-account:" + username) }

func (r *Repository) BeginPasswordLogin(ctx context.Context, username, ip string) (err error) {
	ctx, span := platform.Start(ctx, "iam.repository.BeginPasswordLogin")
	defer span.End()
	defer func() { platform.TraceError(ctx, err) }()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scopes := []struct {
			id    string
			limit int
		}{{passwordRateID(username), 10}}
		if ip != "" {
			scopes = append(scopes, struct {
				id    string
				limit int
			}{authKey("password-ip:" + ip), 60})
		}
		now := time.Now().UTC()
		for _, scope := range scopes {
			row := PasswordRate{ID: scope.id, WindowStarted: now}
			if e := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; e != nil {
				return e
			}
			if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", scope.id).Error; e != nil {
				return e
			}
			if row.BlockedUntil != nil && row.BlockedUntil.After(now) {
				return status.Error(codes.ResourceExhausted, "登录尝试过于频繁，请稍后重试")
			}
			if now.Sub(row.WindowStarted) >= time.Minute {
				row.WindowStarted = now
				row.Attempts = 0
				row.Failures = 0
			}
			if row.Attempts >= scope.limit {
				return status.Error(codes.ResourceExhausted, "登录尝试过于频繁，请稍后重试")
			}
			row.Attempts++
			if e := tx.WithContext(ctx).Save(&row).Error; e != nil {
				return e
			}
		}
		return nil
	})
}

func (r *Repository) FinishPasswordLogin(ctx context.Context, username string, success bool) (err error) {
	ctx, span := platform.Start(ctx, "iam.repository.FinishPasswordLogin")
	defer span.End()
	defer func() { platform.TraceError(ctx, err) }()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row PasswordRate
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", passwordRateID(username)).Error; e != nil {
			return e
		}
		if success {
			row.Failures = 0
			row.BlockedUntil = nil
		} else {
			row.Failures++
			if row.Failures >= 5 {
				until := time.Now().UTC().Add(time.Second * time.Duration(1<<min(row.Failures-5, 6)))
				row.BlockedUntil = &until
			}
		}
		return tx.WithContext(ctx).Save(&row).Error
	})
}
