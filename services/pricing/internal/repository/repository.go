package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/pricing/internal/domain"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db} }
func (r *Repository) Migrate(ctx context.Context) error {
	ctx, s := platform.Start(ctx, "pricing.repository.Migrate")
	defer s.End()
	return r.db.WithContext(ctx).AutoMigrate(&domain.Quote{})
}
func (r *Repository) Create(ctx context.Context, q *domain.Quote) error {
	ctx, s := platform.Start(ctx, "pricing.repository.Create")
	defer s.End()
	return r.db.WithContext(ctx).Create(q).Error
}
func (r *Repository) Get(ctx context.Context, id string) (domain.Quote, error) {
	ctx, s := platform.Start(ctx, "pricing.repository.Get")
	defer s.End()
	var q domain.Quote
	err := r.db.WithContext(ctx).First(&q, "id = ?", id).Error
	return q, err
}
