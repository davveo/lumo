package repository

import (
	"context"
	"github.com/davveo/lumo/services/fulfillment/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func (r *Repository) EnsureReturnReceipts(ctx context.Context, rows []*domain.Record) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := &Repository{DB: tx}
		for _, row := range rows {
			want := *row
			if err := repo.Create(ctx, row); err != nil {
				return err
			}
			if row.Kind != want.Kind || row.OrderID != want.OrderID || row.LinesJSON != want.LinesJSON ||
				row.ReservationID != want.ReservationID {
				return status.Error(codes.AlreadyExists, "inspection id reused with a different receipt")
			}
		}
		return nil
	})
}
