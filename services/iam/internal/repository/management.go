package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var managementSpec = adminquery.Spec{State: "role", Amount: "", Search: []string{"id", "username"}}

func (r *Repository) AdminList(ctx context.Context, c platform.Request) (platform.Result, error) {
	rows, total, filter, err := adminquery.Page[domain.Account](ctx, r.db, c.Data, managementSpec)
	if err != nil {
		return nil, err
	}
	return adminquery.Result(rows, total, filter), nil
}
func (r *Repository) AdminSummary(ctx context.Context, c platform.Request) (platform.Result, error) {
	return adminquery.Summary[domain.Account](ctx, r.db, c.Data, managementSpec)
}

func (r *Repository) AuditList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "iam.repository.AuditList")
	defer sp.End()
	db := r.db.WithContext(ctx)
	if c.ID != "" {
		db = db.Where("user_id = ?", c.ID)
	}
	rows, total, f, e := adminquery.Page[domain.AccountChange](ctx, db, c.Data, adminquery.Spec{State: "active", Search: []string{"id", "user_id", "actor_id", "reason"}})
	return adminquery.Result(rows, total, f), e
}
func (r *Repository) AdminGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "iam.repository.AdminGet")
	defer sp.End()
	var row domain.Account
	result := r.db.WithContext(ctx).Where("id = ?", c.ID).Find(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	return platform.Map(row), nil
}
