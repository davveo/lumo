package repository

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/checkout/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db} }
func (r *Repository) Migrate(ctx context.Context) (spanErr0 error) {
	if e := platform.MigrateBusinessIDs(ctx, r.db); e != nil {
		return e
	}
	ctx, s := platform.Start(ctx, "checkout.repository.Migrate")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).AutoMigrate(&domain.Saga{})
}
func (r *Repository) Get(ctx context.Context, id string) (spanResult0 domain.Saga, spanErr0 error) {
	ctx, s := platform.Start(ctx, "checkout.repository.Get")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var a domain.Saga
	err := r.db.WithContext(ctx).First(&a, "id = ?", id).Error
	return a, err
}
func (r *Repository) Ensure(ctx context.Context, a *domain.Saga) (spanErr0 error) {
	ctx, s := platform.Start(ctx, "checkout.repository.Ensure")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(a).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).First(a, "request_key = ?", a.RequestKey).Error
}
func (r *Repository) Claim(ctx context.Context, id, owner string) (spanResult0 bool, spanErr0 error) {
	ctx, s := platform.Start(ctx, "checkout.repository.Claim")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	now := time.Now().UTC()
	x := r.db.WithContext(ctx).
		Model(&domain.Saga{}).
		Where("id = ? AND (lease_until < ? OR lease_owner = '')", id, now).
		Updates(map[string]any{"lease_owner": owner, "lease_until": now.Add(2 * time.Minute)})
	return x.RowsAffected == 1, x.Error
}
func (r *Repository) Release(ctx context.Context, id, owner string) (spanErr0 error) {
	ctx, s := platform.Start(ctx, "checkout.repository.Release")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).
		Model(&domain.Saga{}).
		Where("id = ? AND lease_owner = ?", id, owner).
		Updates(map[string]any{"lease_owner": "", "lease_until": time.Now().UTC()}).
		Error
}
func (r *Repository) Save(ctx context.Context, a *domain.Saga, owner string) (spanErr0 error) {
	ctx, s := platform.Start(ctx, "checkout.repository.Save")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	x := r.db.WithContext(ctx).
		Model(&domain.Saga{}).
		Where("id = ? AND lease_owner = ?", a.ID, owner).
		Updates(map[string]any{"state": a.State, "steps": a.Steps, "last_error": a.LastError})
	if x.Error != nil {
		return x.Error
	}
	if x.RowsAffected == 0 {
		return gorm.ErrInvalidTransaction
	}
	return nil
}
func (r *Repository) Pending(ctx context.Context) (spanResult0 []domain.Saga, spanErr0 error) {
	ctx, s := platform.Start(ctx, "checkout.repository.Pending")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var rows []domain.Saga
	err := r.db.WithContext(ctx).
		Where(
			"state IN ?",
			[]string{
				constants.CheckoutStateCreating,
				constants.CheckoutStateCancelling,
				constants.CheckoutStatePaidConfirming,
				constants.CheckoutStatePaidWaitGroup,
			},
		).
		Order("updated_at").
		Limit(30).
		Find(&rows).
		Error
	return rows, err
}

func (r *Repository) ByRequestKey(ctx context.Context, key string) (spanResult0 domain.Saga, spanErr0 error) {
	ctx, span := platform.Start(ctx, "checkout.repository.ByRequestKey")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var row domain.Saga
	e := r.db.WithContext(ctx).First(&row, "request_key = ?", key).Error
	return row, e
}
