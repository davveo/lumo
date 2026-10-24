package repository

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"strings"

	"github.com/davveo/lumo/services/catalog/internal/domain"
)

// projectedProductQuery keeps the normal branch covered by a small secondary
// index. The disjoint long-document branch evaluates the original JSON values.
// Each branch takes at most the requested final limit under the same ordering;
// their union is sorted and limited again before loading full product payloads.
// The top K of a union must be contained in the top K of each ordered branch.
func projectedProductQuery(input ProductSearch) (string, []any) {
	text := func(path string) string {
		return "JSON_UNQUOTE(JSON_EXTRACT(CASE WHEN JSON_VALID(payload) THEN payload ELSE '{}' END, '" + path + "'))"
	}
	where := func(long bool) (string, []any) {
		clauses := []string{"search_public_v2 = 1"}
		args := []any{}
		if long {
			clauses = append(clauses, "search_long_v2 = 1")
		} else {
			clauses = append(clauses, "search_long_v2 = 0")
		}
		for _, field := range []struct{ value, indexed, path string }{
			{input.Category, "search_category_v2", "$.category"},
			{input.Brand, "search_brand_v2", "$.brand"},
		} {
			if field.value == "" {
				continue
			}
			expression := field.indexed
			if long {
				expression = text(field.path)
			}
			clauses = append(clauses, expression+" = ?")
			args = append(args, field.value)
		}
		if input.Query != "" {
			value := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").
				Replace(strings.ToLower(input.Query)) +
				"%"
			if !long {
				clauses = append(clauses, "search_text_v2 LIKE ? ESCAPE '!'")
				args = append(args, value)
			} else {
				matches := []string{}
				for _, path := range []string{"$.name", "$.brand", "$.category"} {
					matches = append(matches, "LOWER(COALESCE("+text(path)+", '')) LIKE ? ESCAPE '!'")
					args = append(args, value)
				}
				clauses = append(clauses, "("+strings.Join(matches, " OR ")+")")
			}
		}
		return strings.Join(clauses, " AND "), args
	}
	field, direction := "", ""
	switch input.Sort {
	case constants.CatalogSortPriceAsc:
		field, direction = "price", "ASC"
	case constants.CatalogSortPriceDesc:
		field, direction = "price", "DESC"
	case constants.CatalogSortSales, constants.CatalogSortSalesDesc:
		field, direction = "sales", "DESC"
	}
	order := "created_at DESC, id DESC"
	if field != "" {
		order = "sort_value " + direction + ", " + order
	}
	branch := func(long bool) (string, []any) {
		rank := "0"
		if field != "" {
			expression := "search_" + field + "_v2"
			if long {
				path := "$.sales"
				if field == "price" {
					path = "$.price_cents"
				}
				expression = "COALESCE(" + text(path) + ", 0)"
			}
			rank = "CAST(" + expression + " AS SIGNED)"
		}
		conditions, args := where(long)
		sql := "(SELECT id, created_at, " + rank + " AS sort_value FROM catalog_records FORCE INDEX (" +
			searchDocumentIndex +
			") WHERE " +
			conditions +
			" ORDER BY " +
			order +
			" LIMIT ?)"
		return sql, append(args, input.Limit)
	}
	regular, regularArgs := branch(false)
	long, longArgs := branch(true)
	args := append(regularArgs, longArgs...)
	args = append(args, input.Limit)
	candidates := "SELECT id, created_at, sort_value FROM (" + regular + " UNION ALL " + long +
		") AS branches ORDER BY " +
		order +
		" LIMIT ?"
	finalOrder := "hits.created_at DESC, hits.id DESC"
	if field != "" {
		finalOrder = "hits.sort_value " + direction + ", " + finalOrder
	}
	columns := "catalog_records.id, catalog_records.user_id, catalog_records.parent_id, catalog_records.kind, catalog_records.status, catalog_records.payload, catalog_records.created_at, catalog_records.updated_at"
	return "SELECT " + columns + " FROM (" + candidates +
		") AS hits JOIN catalog_records ON catalog_records.id = hits.id ORDER BY " +
		finalOrder, args
}

func (r *Repository) searchProjectedProducts(ctx context.Context, input ProductSearch) ([]domain.Record, error) {
	sql, args := projectedProductQuery(input)
	rows := []domain.Record{}
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
}
