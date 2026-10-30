package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/content/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) PopupSettings(ctx context.Context) (spanResult0 domain.PopupSettings, spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.PopupSettings")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	v := domain.PopupSettings{ID: "global", Enabled: true, MaxPerSession: 1, MinGapSeconds: 60}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&v).Error; err != nil {
		return v, err
	}
	err := r.db.WithContext(ctx).First(&v, "id = ?", "global").Error
	return v, err
}
func (r *Repository) SavePopupSettings(ctx context.Context, v domain.PopupSettings) (spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.SavePopupSettings")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	v.ID = "global"
	return r.db.WithContext(ctx).
		Model(&domain.PopupSettings{}).
		Where("id = ?", "global").
		Updates(
			map[string]any{
				"enabled":         v.Enabled,
				"max_per_session": v.MaxPerSession,
				"min_gap_seconds": v.MinGapSeconds,
				"updated_at":      time.Now().UTC(),
			},
		).
		Error
}
func (r *Repository) PopupCampaigns(ctx context.Context, published bool) (spanResult0 []domain.PopupCampaign, spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.PopupCampaigns")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	rows := []domain.PopupCampaign{}
	q := r.db.WithContext(ctx)
	if published {
		q = q.Where("status = ?", constants.PopupStatusPublished)
	}
	err := q.Order("priority DESC, id ASC").Find(&rows).Error
	return rows, err
}
func (r *Repository) PopupCampaign(ctx context.Context, id string) (spanResult0 domain.PopupCampaign, spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.PopupCampaign")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var v domain.PopupCampaign
	err := r.db.WithContext(ctx).First(&v, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = status.Error(codes.NotFound, "popup campaign not found")
	}
	return v, err
}
func (r *Repository) SavePopupCampaign(ctx context.Context, v *domain.PopupCampaign, insertOnly bool) (spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.SavePopupCampaign")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := *v
		seed.Version = 1
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed)
		if result.Error != nil {
			return result.Error
		}
		if insertOnly || result.RowsAffected == 1 {
			*v = seed
			return nil
		}
		var old domain.PopupCampaign
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", v.ID).Error; err != nil {
			return err
		}
		if v.Version < 1 || v.Version != old.Version {
			return status.Error(codes.Aborted, "popup configuration changed; reload before saving")
		}
		old.Name = v.Name
		old.Status = v.Status
		old.Priority = v.Priority
		old.Payload = v.Payload
		old.Version++
		if err := tx.Save(&old).Error; err != nil {
			return err
		}
		*v = old
		return nil
	})
}
func (r *Repository) PopupVisitor(ctx context.Context, id, session string) (spanResult0 domain.PopupVisitor, spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.PopupVisitor")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	v := domain.PopupVisitor{ID: id, FirstSession: session}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&v).Error; err != nil {
		return v, err
	}
	err := r.db.WithContext(ctx).First(&v, "id = ?", id).Error
	return v, err
}
func (r *Repository) CreatePopupDelivery(ctx context.Context, v *domain.PopupDelivery) (spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.CreatePopupDelivery")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).Create(v).Error
}
func (r *Repository) PopupDelivery(ctx context.Context, id string) (spanResult0 domain.PopupDelivery, spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.PopupDelivery")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var v domain.PopupDelivery
	err := r.db.WithContext(ctx).First(&v, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = status.Error(codes.NotFound, "popup delivery not found")
	}
	return v, err
}
func (r *Repository) PopupCounter(ctx context.Context, id string) (spanResult0 domain.PopupCounter, spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.PopupCounter")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	v := domain.PopupCounter{ID: id}
	err := r.db.WithContext(ctx).First(&v, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.PopupCounter{ID: id}, nil
	}
	return v, err
}
func (r *Repository) PopupSubject(ctx context.Context, id string) (spanResult0 domain.PopupSubject, spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.PopupSubject")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	v := domain.PopupSubject{ID: id}
	err := r.db.WithContext(ctx).First(&v, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.PopupSubject{ID: id}, nil
	}
	return v, err
}

// PopupTransaction always locks the subject before the campaign and delivery.
// This ordering serializes all impressions for the subject across replicas and
// prevents two tabs/campaigns consuming the same session allowance.
func (r *Repository) PopupTransaction(
	ctx context.Context,
	subject, campaign, delivery string,
	fn func(*PopupTx, *domain.PopupSubject, *domain.PopupCampaign, *domain.PopupDelivery) error,
) (spanErr0 error) {
	ctx, span := platform.Start(ctx, "content.repository.PopupTransaction")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		u := domain.PopupSubject{ID: subject}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&u).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, "id = ?", subject).Error; err != nil {
			return err
		}
		var c domain.PopupCampaign
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ?", campaign).Error; err != nil {
			return err
		}
		var d domain.PopupDelivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&d, "id = ?", delivery).Error; err != nil {
			return err
		}
		return fn(&PopupTx{db: tx}, &u, &c, &d)
	})
}

type PopupTx struct{ db *gorm.DB }

func (t *PopupTx) Settings() (domain.PopupSettings, error) {
	var v domain.PopupSettings
	err := t.db.First(&v, "id = ?", "global").Error
	return v, err
}
func (t *PopupTx) Save(value any) error { return t.db.Save(value).Error }
func (t *PopupTx) Counter(id string) (domain.PopupCounter, error) {
	v := domain.PopupCounter{ID: id}
	if err := t.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&v).Error; err != nil {
		return v, err
	}
	err := t.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&v, "id = ?", id).Error
	return v, err
}
