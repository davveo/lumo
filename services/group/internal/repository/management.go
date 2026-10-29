package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/group/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *Repository) AdminList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "group.repository.AdminList")
	defer sp.End()
	rows, total, f, e := adminquery.Page[domain.Team](ctx, r.DB, c.Data, adminquery.Spec{State: "state", Search: []string{"id", "sku_id", "creator_id", "rule_id", "activity_id"}})
	return adminquery.Result(rows, total, f), e
}
func (r *Repository) RulesAdmin(ctx context.Context, c platform.Request, public bool) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "group.repository.RulesAdmin")
	defer sp.End()
	db := r.DB.WithContext(ctx)
	if public {
		db = db.Where("active = ?", true)
	}
	rows, total, f, e := adminquery.Page[domain.Rule](ctx, db, c.Data, adminquery.Spec{State: "active", Search: []string{"id", "name", "sku_id", "activity_id"}})
	return adminquery.Result(rows, total, f), e
}
func (r *Repository) Rule(ctx context.Context, id string) (domain.Rule, error) {
	ctx, sp := platform.Start(ctx, "group.repository.Rule")
	defer sp.End()
	var v domain.Rule
	e := r.DB.WithContext(ctx).First(&v, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return v, status.Error(codes.NotFound, "group rule not found")
	}
	return v, e
}
func (r *Repository) RuleUpsert(ctx context.Context, next domain.Rule, actor, reason string) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "group.repository.RuleUpsert")
	defer sp.End()
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old domain.Rule
		e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", next.ID).Error
		missing := errors.Is(e, gorm.ErrRecordNotFound)
		if e != nil && !missing {
			return e
		}
		if old.Version != next.Version {
			return status.Error(codes.Aborted, "group rule changed; reload before saving")
		}
		next.Version++
		next.UpdatedAt = time.Now().UTC()
		if missing {
			v := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&next)
			if v.Error != nil {
				return v.Error
			}
			if v.RowsAffected != 1 {
				return status.Error(codes.Aborted, "group rule already exists")
			}
		} else {
			next.CreatedAt = old.CreatedAt
			v := tx.WithContext(ctx).Model(&domain.Rule{}).Where("id = ? AND version = ?", next.ID, old.Version).Select("name", "sku_id", "activity_id", "required", "duration_minutes", "active", "starts_at", "ends_at", "version", "updated_at").Updates(&next)
			if v.Error != nil {
				return v.Error
			}
			if v.RowsAffected != 1 {
				return status.Error(codes.Aborted, "group rule changed")
			}
		}
		before, _ := json.Marshal(old)
		after, _ := json.Marshal(next)
		return tx.WithContext(ctx).Create(&adminconfig.Change{ID: platform.ID("group-change"), DocumentID: next.ID, Version: next.Version, ActorID: actor, Reason: reason, Before: string(before), After: string(after)}).Error
	})
	return platform.Map(next), e
}

func (r *Repository) PublicRules(ctx context.Context) ([]domain.Rule, error) {
	ctx, sp := platform.Start(ctx, "group.repository.PublicRules")
	defer sp.End()
	rows := []domain.Rule{}
	e := r.DB.WithContext(ctx).Where("active = ?", true).Order("id").Find(&rows).Error
	return rows, e
}
