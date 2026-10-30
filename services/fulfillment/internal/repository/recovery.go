package repository

import (
	"context"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/fulfillment/internal/domain"
)

func (r *Repository) Pending(ctx context.Context) (spanResult0 []domain.Record, spanErr0 error) {
	ctx, span := platform.Start(ctx, "repository.fulfillment.PendingRecovery")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	rows := []domain.Record{}
	err := r.DB.WithContext(ctx).
		Where(
			"status IN ?",
			[]string{
				constants.FulfillmentStatusDispatching,
				constants.FulfillmentStatusCancelRequested,
				constants.FulfillmentStatusWarehouseCancelConfirmed,
			},
		).
		Order("updated_at asc").
		Limit(50).
		Find(&rows).
		Error
	return rows, err
}
