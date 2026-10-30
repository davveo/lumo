package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/flash/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *Repository) AdminList(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "flash.repository.AdminList")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	rows, total, f, err := adminquery.Page[domain.Session](ctx, r.DB, c.Data, adminquery.Spec{State: "active", Search: []string{"id", "name", "sku_id", "activity_id"}})
	return adminquery.Result(rows, total, f), err
}
func (r *Repository) RequestsAdmin(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "flash.repository.RequestsAdmin")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	rows, total, f, err := adminquery.Page[domain.Request](ctx, r.DB, c.Data, adminquery.Spec{State: "state", Search: []string{"id", "user_id", "order_id", "session_id", "error"}})
	return adminquery.Result(rows, total, f), err
}
func (r *Repository) SessionForActivity(ctx context.Context, id string) (spanResult0 domain.Session, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "flash.repository.SessionForActivity")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var row domain.Session
	err := r.DB.WithContext(ctx).First(&row, "activity_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, status.Error(codes.NotFound, "flash session not found")
	}
	return row, err
}
func (r *Repository) Upsert(ctx context.Context, next domain.Session, actor, reason string) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "flash.repository.Upsert")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old domain.Session
		err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", next.ID).Error
		missing := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !missing {
			return err
		}
		if old.Version != next.Version {
			return status.Error(codes.Aborted, "session changed; reload before saving")
		}
		if !missing && (old.SKUID != next.SKUID || old.ActivityID != next.ActivityID) {
			return status.Error(codes.FailedPrecondition, "SKU and linked activity are immutable; create a new session")
		}
		next.Version++
		next.UpdatedAt = time.Now().UTC()
		if missing {
			result := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&next)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return status.Error(codes.Aborted, "session already exists")
			}
		} else {
			next.CreatedAt = old.CreatedAt
			result := tx.WithContext(ctx).Model(&domain.Session{}).Where("id = ? AND version = ?", old.ID, old.Version).Select("name", "starts_at", "ends_at", "active", "version", "updated_at").Updates(&next)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return status.Error(codes.Aborted, "session changed")
			}
		}
		before, _ := json.Marshal(old)
		after, _ := json.Marshal(next)
		return tx.WithContext(ctx).Create(&adminconfig.Change{ID: platform.ID("flash-change"), DocumentID: next.ID, Version: next.Version, ActorID: actor, Reason: reason, Before: string(before), After: string(after)}).Error
	})
	return platform.Map(next), err
}
