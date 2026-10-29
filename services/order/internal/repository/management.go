package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"
)

var managementSpec = adminquery.Spec{
	State:  "status",
	Amount: "payable",
	Search: []string{"id", "user_id", "payment_id", "note"},
}

func (r *Repository) AdminList(
	ctx context.Context,
	data map[string]any,
) ([]domain.Order, int64, adminquery.Filters, error) {
	return adminquery.Page[domain.Order](ctx, r.db, data, managementSpec)
}
func (r *Repository) AdminSummary(ctx context.Context, data map[string]any) (platform.Result, error) {
	return adminquery.Summary[domain.Order](ctx, r.db, data, managementSpec)
}
