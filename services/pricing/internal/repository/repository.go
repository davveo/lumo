package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/pricing/internal/domain"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db} }
func (r *Repository) Migrate(ctx context.Context) (spanErr0 error) {
	if e := platform.MigrateBusinessIDs(ctx, r.db); e != nil {
		return e
	}
	ctx, s := platform.Start(ctx, "pricing.repository.Migrate")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).AutoMigrate(&domain.Quote{})
}
func (r *Repository) Create(ctx context.Context, q *domain.Quote) (spanErr0 error) {
	ctx, s := platform.Start(ctx, "pricing.repository.Create")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).Create(q).Error
}
func (r *Repository) Get(ctx context.Context, id string) (spanResult0 domain.Quote, spanErr0 error) {
	ctx, s := platform.Start(ctx, "pricing.repository.Get")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var q domain.Quote
	err := r.db.WithContext(ctx).First(&q, "id = ?", id).Error
	return q, err
}
