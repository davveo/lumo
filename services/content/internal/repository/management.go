package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/content/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *Repository) AdminList(ctx context.Context, c platform.Request) ([]domain.Record, int64, adminquery.Filters, error) {
	ctx, sp := platform.Start(ctx, "content.repository.AdminList")
	defer sp.End()
	db := r.db.WithContext(ctx).Where("kind IN ?", []string{constants.ContentKindNotice, constants.ContentKindFaq})
	if kind := platform.Text(c.Data, "kind"); kind != "" {
		db = db.Where("kind = ?", kind)
	}
	return adminquery.Page[domain.Record](ctx, db, c.Data, adminquery.Spec{State: "status", Search: []string{"id", "payload"}})
}
func (r *Repository) AdminUpsert(ctx context.Context, next domain.Record, actor, reason string) (domain.Record, error) {
	ctx, sp := platform.Start(ctx, "content.repository.AdminUpsert")
	defer sp.End()
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old domain.Record
		e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", next.ID).Error
		missing := errors.Is(e, gorm.ErrRecordNotFound)
		if e != nil && !missing {
			return e
		}
		if next.Version != old.Version {
			return status.Error(codes.Aborted, "content changed; reload before saving")
		}
		if !missing && old.Kind != next.Kind {
			return status.Error(codes.FailedPrecondition, "content kind cannot change")
		}
		next.Version++
		next.UpdatedAt = time.Now().UTC()
		if missing {
			v := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&next)
			if v.Error != nil {
				return v.Error
			}
			if v.RowsAffected != 1 {
				return status.Error(codes.Aborted, "content already exists")
			}
		} else {
			next.CreatedAt = old.CreatedAt
			v := tx.WithContext(ctx).Model(&domain.Record{}).Where("id = ? AND version = ?", old.ID, old.Version).Select("payload", "status", "version", "publish_at", "unpublish_at", "updated_at").Updates(&next)
			if v.Error != nil {
				return v.Error
			}
			if v.RowsAffected != 1 {
				return status.Error(codes.Aborted, "content changed")
			}
		}
		before, _ := json.Marshal(old)
		after, _ := json.Marshal(next)
		return tx.WithContext(ctx).Create(&adminconfig.Change{ID: platform.ID("content-change"), DocumentID: next.ID, Version: next.Version, ActorID: actor, Reason: reason, Before: string(before), After: string(after)}).Error
	})
	return next, e
}
