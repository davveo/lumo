package repository

import (
	"context"
	"errors"
	"time"

	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AlertPolicy struct {
	PriceEnabled bool  `json:"price_enabled"`
	StockEnabled bool  `json:"stock_enabled"`
	TTLDays      int64 `json:"ttl_days"`
	MaxPerUser   int64 `json:"max_per_user"`
}

func (r *Repository) AlertPolicy(ctx context.Context) (AlertPolicy, int64, error) {
	return adminconfig.Read(ctx, r.db, "product-alerts", AlertPolicy{PriceEnabled: true, StockEnabled: true, TTLDays: 30, MaxPerUser: 30})
}
func (r *Repository) SaveAlertPolicy(ctx context.Context, c platform.Request, p AlertPolicy) (platform.Result, error) {
	return adminconfig.Save(ctx, r.db, "product-alerts", p, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
}

func (r *Repository) Subscribe(ctx context.Context, seed domain.Subscription, max int64) (domain.Subscription, error) {
	ctx, sp := platform.Start(ctx, "engagement.repository.Subscribe")
	defer sp.End()
	var got domain.Subscription
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		owner := domain.CompareOwner{UserID: seed.UserID}
		if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&owner).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&owner, "user_id = ?", seed.UserID).Error; err != nil {
			return err
		}
		err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&got, "slot_key = ?", seed.SlotKey).Error
		missing := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !missing {
			return err
		}
		now := time.Now().UTC()
		if !missing && got.Status == constants.SubscriptionStatusActive && got.ExpiresAt.After(now) {
			// A retry cannot extend its lifetime or change the target behind the
			// owner's back. Cancel the old subscription before changing its target.
			if got.TargetCents != seed.TargetCents {
				return status.Error(codes.AlreadyExists, "cancel the existing subscription before changing its target")
			}
			return nil
		}
		var count int64
		if err = tx.WithContext(ctx).Model(&domain.Subscription{}).Where("user_id = ? AND status = ? AND expires_at > ?", seed.UserID, constants.SubscriptionStatusActive, now).Count(&count).Error; err != nil {
			return err
		}
		if count >= max {
			return status.Error(codes.ResourceExhausted, "active product subscription limit reached")
		}
		if missing {
			seed.ID, err = platform.NextBusinessID(ctx, tx, "alert")
			if err != nil {
				return err
			}
			seed.Generation = 1
			got = seed
			return tx.WithContext(ctx).Create(&got).Error
		}
		seed.ID = got.ID
		seed.Generation = got.Generation + 1
		seed.CreatedAt = got.CreatedAt
		got = seed
		return tx.WithContext(ctx).Save(&got).Error
	})
	return got, err
}

func (r *Repository) Subscriptions(ctx context.Context, owner, sku, kind, state string, limit, offset int) ([]domain.Subscription, int64, error) {
	ctx, sp := platform.Start(ctx, "engagement.repository.Subscriptions")
	defer sp.End()
	q := r.db.WithContext(ctx).Model(&domain.Subscription{})
	if owner != "" {
		q = q.Where("user_id = ?", owner)
	}
	if sku != "" {
		q = q.Where("sku_id = ?", sku)
	}
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if state != "" {
		q = q.Where("status = ?", state)
	}
	var total int64
	rows := []domain.Subscription{}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("updated_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

func (r *Repository) CancelSubscription(ctx context.Context, c platform.Request, admin bool) (domain.Subscription, error) {
	ctx, sp := platform.Start(ctx, "engagement.repository.CancelSubscription")
	defer sp.End()
	var got domain.Subscription
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"})
		if !admin {
			q = q.Where("user_id = ?", c.UserID)
		}
		if err := q.First(&got, "id = ?", c.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return status.Error(codes.NotFound, "subscription not found")
			}
			return err
		}
		if got.Status != constants.SubscriptionStatusActive {
			return nil
		}
		got.Status = constants.SubscriptionStatusCancelled
		got.CancelledBy = c.UserID
		got.CancelReason = platform.Text(c.Data, "reason")
		if admin {
			if err := tx.WithContext(ctx).Create(&adminconfig.Change{ID: platform.ID("audit"), DocumentID: "product-alert:" + got.ID, Version: got.Generation, ActorID: c.UserID, Reason: got.CancelReason, Before: constants.SubscriptionStatusActive, After: got.Status}).Error; err != nil {
				return err
			}
		}
		return tx.WithContext(ctx).Save(&got).Error
	})
	return got, err
}

func (r *Repository) DueSubscriptions(ctx context.Context, now time.Time) ([]domain.Subscription, error) {
	ctx, sp := platform.Start(ctx, "engagement.repository.DueSubscriptions")
	defer sp.End()
	rows := []domain.Subscription{}
	err := r.db.WithContext(ctx).Where("status = ? AND next_check_at <= ?", constants.SubscriptionStatusActive, now).Order("next_check_at, id").Limit(100).Find(&rows).Error
	return rows, err
}

// CompleteCheck compares the generation and check time captured before RPC.
// A cancelled/rearmed subscription or another replica can never emit twice.
func (r *Repository) CompleteCheck(ctx context.Context, expected domain.Subscription, now time.Time, price int64, hit bool) error {
	ctx, sp := platform.Start(ctx, "engagement.repository.CompleteCheck")
	defer sp.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var got domain.Subscription
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&got, "id = ?", expected.ID).Error; err != nil {
			return err
		}
		if got.Status != constants.SubscriptionStatusActive || got.Generation != expected.Generation || !got.NextCheckAt.Equal(expected.NextCheckAt) {
			return nil
		}
		got.NextCheckAt = now.Add(5 * time.Minute)
		if !got.ExpiresAt.After(now) {
			got.Status = constants.SubscriptionStatusExpired
			hit = false
		}
		if hit {
			got.Status = constants.SubscriptionStatusTriggered
			got.TriggeredAt = &now
			got.PriceCents = price
			kind := constants.EventProductPriceReached
			if got.Kind == constants.SubscriptionKindStock {
				kind = constants.EventProductBackInStock
			}
			if err := platform.Enqueue(ctx, tx, platform.Event{ID: platform.ID("evt"), Type: kind, AggregateID: got.ID, UserID: got.UserID, Data: map[string]any{"sku_id": got.SKUID, "product_name": got.ProductName, "amount_cents": price}}); err != nil {
				return err
			}
		}
		return tx.WithContext(ctx).Save(&got).Error
	})
}
