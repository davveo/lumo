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
func (r *Repository) Migrate(ctx context.Context) error {
	ctx, s := platform.Start(ctx, "checkout.repository.Migrate")
	defer s.End()
	return r.db.WithContext(ctx).AutoMigrate(&domain.Saga{})
}
func (r *Repository) Get(ctx context.Context, id string) (domain.Saga, error) {
	ctx, s := platform.Start(ctx, "checkout.repository.Get")
	defer s.End()
	var a domain.Saga
	err := r.db.WithContext(ctx).First(&a, "id = ?", id).Error
	return a, err
}
func (r *Repository) Ensure(ctx context.Context, a *domain.Saga) error {
	ctx, s := platform.Start(ctx, "checkout.repository.Ensure")
	defer s.End()
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(a).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).First(a, "id = ?", a.ID).Error
}
func (r *Repository) Claim(ctx context.Context, id, owner string) (bool, error) {
	ctx, s := platform.Start(ctx, "checkout.repository.Claim")
	defer s.End()
	now := time.Now().UTC()
	x := r.db.WithContext(ctx).
		Model(&domain.Saga{}).
		Where("id = ? AND (lease_until < ? OR lease_owner = '')", id, now).
		Updates(map[string]any{"lease_owner": owner, "lease_until": now.Add(2 * time.Minute)})
	return x.RowsAffected == 1, x.Error
}
func (r *Repository) Release(ctx context.Context, id, owner string) error {
	ctx, s := platform.Start(ctx, "checkout.repository.Release")
	defer s.End()
	return r.db.WithContext(ctx).
		Model(&domain.Saga{}).
		Where("id = ? AND lease_owner = ?", id, owner).
		Updates(map[string]any{"lease_owner": "", "lease_until": time.Now().UTC()}).
		Error
}
func (r *Repository) Save(ctx context.Context, a *domain.Saga, owner string) error {
	ctx, s := platform.Start(ctx, "checkout.repository.Save")
	defer s.End()
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
func (r *Repository) Pending(ctx context.Context) ([]domain.Saga, error) {
	ctx, s := platform.Start(ctx, "checkout.repository.Pending")
	defer s.End()
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
