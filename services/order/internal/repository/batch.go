package repository

import (
	"context"
	"github.com/davveo/lumo/services/order/internal/domain"
)

func (r *Repository) GetMany(ctx context.Context, ids []string) ([]domain.Order, error) {
	rows := []domain.Order{}
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Order("id").Find(&rows).Error
	return rows, err
}
