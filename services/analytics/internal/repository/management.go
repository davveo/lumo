package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/domain"
)

func (r *Repository) AdminList(ctx context.Context, c platform.Request) (spanResult0 []domain.Record, spanResult1 int64, spanResult2 adminquery.Filters, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "analytics.repository.AdminList")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	q := r.db.WithContext(ctx)
	if v := platform.Text(c.Data, "event_type"); v != "" {
		expr := "JSON_UNQUOTE(JSON_EXTRACT(payload,'$.type'))"
		if r.db.Dialector.Name() == "sqlite" {
			expr = "JSON_EXTRACT(payload,'$.type')"
		}
		q = q.Where("CASE WHEN JSON_VALID(payload) THEN "+expr+" ELSE '' END = ?", v)
	}
	return adminquery.Page[domain.Record](ctx, q, c.Data, adminquery.Spec{State: "kind", Search: []string{"id", "user_id", "payload"}})
}
