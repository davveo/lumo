package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/fulfillment/internal/domain"
)

func (r *Repository) Pending(ctx context.Context) ([]domain.Record, error) {
	ctx, span := platform.Start(ctx, "repository.fulfillment.PendingRecovery")
	defer span.End()
	rows := []domain.Record{}
	err := r.DB.WithContext(ctx).Where("status IN ?", []string{"DISPATCHING", "CANCEL_REQUESTED", "WAREHOUSE_CANCEL_CONFIRMED"}).Order("updated_at asc").Limit(50).Find(&rows).Error
	return rows, err
}
