package repository

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/domain"
)

func (r *Repository) Pending(ctx context.Context) ([]domain.Record, error) {
	ctx, span := platform.Start(ctx, "repository.aftersale.PendingRecovery")
	defer span.End()
	rows := []domain.Record{}
	err := r.DB.WithContext(ctx).
		Where(
			"status IN ?",
			[]string{
				constants.AftersaleStatusInspecting,
				constants.AftersaleStatusRefundRequested,
				constants.AftersaleStatusExchangePreparing,
			},
		).
		Order("updated_at asc").
		Limit(50).
		Find(&rows).
		Error
	return rows, err
}
