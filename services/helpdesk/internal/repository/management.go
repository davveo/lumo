package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/domain"
	"time"
)

func (r *Repository) AdminList(
	ctx context.Context,
	c platform.Request,
) (spanResult0 []domain.Record, spanResult1 int64, spanResult2 adminquery.Filters, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "helpdesk.repository.AdminList")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return adminquery.Page[domain.Record](
		ctx,
		r.db.WithContext(ctx).Where("kind = ?", constants.HelpdeskKindTicket),
		c.Data,
		adminquery.Spec{Owner: "user_id", ID: "id", State: "status", Search: []string{"id", "user_id", "payload"}},
	)
}

func (r *Repository) MessagePage(
	ctx context.Context,
	parent string,
	limit, offset int,
) (spanResult0 []domain.Record, spanResult1 int64, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "helpdesk.repository.MessagePage")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	rows := []domain.Record{}
	q := r.db.WithContext(ctx).
		Model(&domain.Record{}).
		Where("parent_id = ? AND kind = ?", parent, constants.HelpdeskKindMessage)
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, 0, e
	}
	err := q.Order("created_at ASC,id ASC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

func (r *Repository) ForOrder(
	ctx context.Context,
	user, id string,
	c platform.Request,
) ([]domain.Record, platform.Result, error) {
	path := "JSON_EXTRACT(payload,'$.order_id')"
	if r.db.Dialector.Name() == "mysql" {
		path = "JSON_UNQUOTE(" + path + ")"
	}
	q := r.db.Model(&domain.Record{}).
		Where("kind = ? AND (parent_id = ? OR (parent_id = '' AND "+path+" = ?))", constants.HelpdeskKindTicket, id, id)
	if user != "" {
		q = q.Where("user_id = ?", user)
	}
	return platform.ReadListPage(
		ctx,
		q,
		c,
		"helpdesk-order:"+id,
		func(r domain.Record) (string, time.Time) { return r.ID, r.CreatedAt },
	)
}
