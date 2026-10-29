package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/invoice/internal/domain"
)

var managementSpec = adminquery.Spec{
	State:  "status",
	Amount: "amount_cents",
	Search: []string{"id", "order_id", "user_id", "title", "number"},
}

func (r *Repository) AdminList(ctx context.Context, c platform.Request) (platform.Result, error) {
	rows, total, filter, err := adminquery.Page[domain.Record](ctx, r.DB, c.Data, managementSpec)
	if err != nil {
		return nil, err
	}
	return adminquery.Result(rows, total, filter), nil
}
func (r *Repository) AdminSummary(ctx context.Context, c platform.Request) (platform.Result, error) {
	return adminquery.Summary[domain.Record](ctx, r.DB, c.Data, managementSpec)
}
