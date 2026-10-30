package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/adminconfig"
	"math"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/campaign/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) AdminList(
	ctx context.Context,
	page, size int,
	keyword, kind string,
	active *bool,
) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "repository.campaign.AdminList")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	query := r.DB.WithContext(ctx).Model(&domain.Activity{})
	if keyword != "" {
		query = query.Where(
			"id LIKE ? OR name LIKE ? OR description LIKE ?",
			"%"+keyword+"%",
			"%"+keyword+"%",
			"%"+keyword+"%",
		)
	}
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if active != nil {
		query = query.Where("active = ?", *active)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	items := []domain.Activity{}
	if err := query.Order("id").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, err
	}
	return platform.Result{"items": items, "total": total, "page": page, "page_size": size}, nil
}

func (r *Repository) Upsert(ctx context.Context, next domain.Activity, dedicated bool, actor, reason string) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "repository.campaign.Upsert")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		audit := func(prior domain.Activity) error {
			if !dedicated {
				return nil
			}
			before, _ := json.Marshal(prior)
			after, _ := json.Marshal(next)
			return tx.WithContext(ctx).Create(&adminconfig.Change{ID: platform.ID("campaign-change"), DocumentID: next.ID, Version: next.Version, ActorID: actor, Reason: reason, Before: string(before), After: string(after)}).Error
		}
		var prior domain.Activity
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&prior, "id = ?", next.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if next.Version != 0 {
				return status.Error(codes.Aborted, "activity no longer exists; reload before saving")
			}
			next.Version = 1
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&next)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected != 1 {
				return status.Error(codes.Aborted, "activity changed; reload before saving")
			}
			return audit(prior)
		}
		if err != nil {
			return err
		}
		if next.Version != prior.Version {
			return status.Error(codes.Aborted, "activity changed; reload before saving")
		}
		if prior.Version == math.MaxInt64 {
			return status.Error(codes.FailedPrecondition, "activity version exhausted; create a new rule")
		}
		// System-backed offers cannot be repurposed through a generic rule editor.
		if !editable(prior.Kind) && !(dedicated && (prior.Kind == constants.CampaignKindFlash || prior.Kind == constants.CampaignKindGroup) && next.Kind == prior.Kind) {
			return status.Error(codes.FailedPrecondition, "system activity is not editable here")
		}
		if prior.Held > 0 || prior.Used > 0 {
			if next.Kind != prior.Kind || next.Threshold != prior.Threshold || next.Discount != prior.Discount ||
				next.PayRateBPS != prior.PayRateBPS || (next.MinQuantity != prior.MinQuantity && !(dedicated && (prior.Kind == constants.CampaignKindFlash || prior.Kind == constants.CampaignKindGroup) && prior.MinQuantity == 0 && next.MinQuantity == 1)) {
				return status.Error(
					codes.FailedPrecondition,
					"used activity pricing cannot change; create a copy instead",
				)
			}
		}
		if prior.Held < 0 || prior.Used < 0 || prior.Held > next.Quota || prior.Used > next.Quota-prior.Held {
			return status.Error(codes.FailedPrecondition, "quota cannot be lower than held and used quantities")
		}
		next.Held, next.Used, next.Version = prior.Held, prior.Used, prior.Version+1
		updated := tx.Model(&domain.Activity{}).
			Where("id = ? AND version = ?", prior.ID, prior.Version).
			Updates(map[string]any{
				"kind": next.Kind, "name": next.Name, "description": next.Description, "threshold": next.Threshold,
				"discount": next.Discount, "pay_rate_bps": next.PayRateBPS, "min_quantity": next.MinQuantity,
				"quota": next.Quota, "active": next.Active, "version": next.Version,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return status.Error(codes.Aborted, "activity changed; reload before saving")
		}
		return audit(prior)
	})
	return platform.Map(next), err
}

func editable(kind string) bool {
	return kind == constants.CampaignKindReduction || kind == constants.CampaignKindDirect ||
		kind == constants.CampaignKindDiscount ||
		kind == constants.CampaignKindQuantityDiscount
}
