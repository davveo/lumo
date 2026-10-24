package repository

import (
	"context"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/reconciliation/internal/domain"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db} }
func (r *Repository) Migrate(ctx context.Context) error {
	ctx, s := platform.Start(ctx, "reconciliation.repository.Migrate")
	defer s.End()
	return r.db.WithContext(ctx).AutoMigrate(&domain.Run{})
}
func (r *Repository) Create(ctx context.Context, x *domain.Run) error {
	ctx, s := platform.Start(ctx, "reconciliation.repository.Create")
	defer s.End()
	return r.db.WithContext(ctx).Create(x).Error
}
func (r *Repository) List(ctx context.Context) ([]domain.Run, error) {
	ctx, s := platform.Start(ctx, "reconciliation.repository.List")
	defer s.End()
	var x []domain.Run
	err := r.db.WithContext(ctx).Order("created_at desc").Limit(30).Find(&x).Error
	return x, err
}
