package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func expirySummary(ctx context.Context, db *gorm.DB, user string, now time.Time) (int64, time.Time, error) {
	q := db.WithContext(ctx).
		Model(&domain.Coupon{}).
		Where("user_id = ? AND state IN ? AND expires_at > ? AND expires_at <= ?", user, []string{constants.CouponStatusAvailable, constants.CouponStatusAvailableLegacy}, now, now.Add(72*time.Hour))
	var count int64
	if e := q.Count(&count).Error; e != nil {
		return 0, time.Time{}, e
	}
	if count == 0 {
		return 0, time.Time{}, nil
	}
	var row domain.Coupon
	if e := q.Order("expires_at ASC,id ASC").First(&row).Error; e != nil {
		return 0, time.Time{}, e
	}
	return count, row.ExpiresAt, nil
}
func (r *Repository) ReminderGet(ctx context.Context, user string) (platform.Result, error) {
	var p domain.ExpiryPreference
	e := r.DB.WithContext(ctx).First(&p, "user_id = ?", user).Error
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	count, at, e := expirySummary(ctx, r.DB, user, time.Now().UTC())
	return platform.Result{
		"enabled":         p.Enabled,
		"expiring_count":  count,
		"next_expiry":     at,
		"window_hours":    72,
		"frequency_hours": 24,
	}, e
}
func (r *Repository) ReminderUpdate(ctx context.Context, user string, enabled bool) (platform.Result, error) {
	p := domain.ExpiryPreference{UserID: user, Enabled: enabled}
	e := r.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"enabled"}),
		}).
		Create(&p).
		Error
	if e != nil {
		return nil, e
	}
	return r.ReminderGet(ctx, user)
}
func (r *Repository) QueueExpiryReminders(ctx context.Context) error {
	now := time.Now().UTC()
	users := []string{}
	if e := r.DB.WithContext(ctx).Table("coupons AS c").Joins("JOIN coupon_expiry_preferences AS p ON p.user_id = c.user_id").Where("p.enabled = ? AND (p.last_queued_at IS NULL OR p.last_queued_at <= ?) AND "+
		"c.state IN ? AND c.expires_at > ? AND c.expires_at <= ?", true, now.Add(-24*time.Hour), []string{constants.CouponStatusAvailable, constants.CouponStatusAvailableLegacy}, now, now.Add(72*time.Hour)).Distinct().Order("c.user_id").Limit(100).Pluck("c.user_id", &users).Error; e != nil {
		return e
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, user := range users {
			var p domain.ExpiryPreference
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, "user_id = ?", user).Error; e != nil {
				return e
			}
			if !p.Enabled || p.LastQueuedAt != nil && now.Sub(*p.LastQueuedAt) < 24*time.Hour {
				continue
			}
			count, at, e := expirySummary(ctx, tx, user, now)
			if e != nil {
				return e
			}
			if count < 1 {
				continue
			}
			dedupe := sha256.Sum256([]byte(user + ":" + now.Format("2006-01-02")))
			event := platform.Event{
				ID:          hex.EncodeToString(dedupe[:]),
				Type:        constants.EventCouponExpiring,
				AggregateID: user,
				UserID:      user,
				Data:        map[string]any{"coupon_count": count, "expires_at": at.UTC().Format(time.RFC3339Nano)},
			}
			if e = platform.Enqueue(ctx, tx, event); e != nil {
				return e
			}
			p.LastQueuedAt = &now
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
		}
		return nil
	})
}
