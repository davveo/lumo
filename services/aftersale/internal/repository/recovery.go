package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/domain"
)

func (r *Repository) Pending(ctx context.Context) ([]domain.Record, error) {
	ctx, span := platform.Start(ctx, "repository.aftersale.PendingRecovery")
	defer span.End()
	rows := []domain.Record{}
	err := r.DB.WithContext(ctx).Where("status IN ?", []string{"INSPECTING", "REFUND_REQUESTED", "EXCHANGE_PREPARING"}).Order("updated_at asc").Limit(50).Find(&rows).Error
	return rows, err
}
