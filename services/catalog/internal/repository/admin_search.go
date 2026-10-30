package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"strings"
)

func (r *Repository) AdminList(ctx context.Context, c platform.Request) (spanResult0 []domain.Record, spanResult1 int64, spanResult2 adminquery.Filters, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "catalog.repository.AdminList")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	data := map[string]any{}
	for k, v := range c.Data {
		data[k] = v
	}
	keyword := strings.TrimSpace(platform.Text(data, "keyword"))
	delete(data, "keyword")
	db := r.db.WithContext(ctx).Where("kind = ? AND parent_id = ?", constants.CatalogKindSku, "")
	text := func(path string) string {
		if r.db.Dialector.Name() == "mysql" {
			return "JSON_UNQUOTE(JSON_EXTRACT(CASE WHEN JSON_VALID(payload) THEN payload ELSE '{}' END, '" + path + "'))"
		}
		return "json_extract(CASE WHEN json_valid(payload) THEN payload ELSE '{}' END, '" + path + "')"
	}
	if keyword != "" {
		like := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(keyword)) + "%"
		db = db.Where("LOWER(id) LIKE ? ESCAPE '!' OR LOWER(COALESCE("+text("$.name")+",'')) LIKE ? ESCAPE '!' OR LOWER(COALESCE("+text("$.brand")+",'')) LIKE ? ESCAPE '!' OR LOWER(COALESCE("+text("$.category")+",'')) LIKE ? ESCAPE '!'", like, like, like, like)
	}
	if category := platform.Text(data, "category"); category != "" {
		db = db.Where(text("$.category")+" = ?", category)
	}
	return adminquery.Page[domain.Record](ctx, db, data, adminquery.Spec{State: "status"})
}
