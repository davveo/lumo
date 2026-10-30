package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/domain"
)

func (r *Repository) AdminList(ctx context.Context, c platform.Request) (spanResult0 []domain.Record, spanResult1 int64, spanResult2 adminquery.Filters, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "helpdesk.repository.AdminList")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return adminquery.Page[domain.Record](ctx, r.db.WithContext(ctx).Where("kind = ?", constants.HelpdeskKindTicket), c.Data, adminquery.Spec{State: "status", Search: []string{"id", "user_id", "payload"}})
}
func (r *Repository) MessagePage(ctx context.Context, parent string, limit, offset int) (spanResult0 []domain.Record, spanResult1 int64, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "helpdesk.repository.MessagePage")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	rows := []domain.Record{}
	q := r.db.WithContext(ctx).Model(&domain.Record{}).Where("parent_id = ? AND kind = ?", parent, constants.HelpdeskKindMessage)
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, 0, e
	}
	err := q.Order("created_at ASC,id ASC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}
