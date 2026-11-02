package repository

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"strings"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"google.golang.org/grpc/status"
)

type ProductSearch struct {
	Query, Category, Brand, Sort string
	Limit                        int
	Cursor                       string
}

// SearchProducts applies the browse constraints before its bounded result page,
// so older matching products are not lost behind the default catalog listing.
// It reads one service-owned table, without COUNT, cross-service stock joins or
// offset walks. Query values remain parameters and LIKE metacharacters literal.
func (r *Repository) SearchProducts(
	ctx context.Context,
	input ProductSearch,
) (spanResult0 []domain.Record, spanErr0 error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ctx, span := platform.Start(ctx, "catalog.repository.SearchProducts")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()
	position, cursorErr := searchCursor(input)
	if cursorErr != nil {
		return nil, cursorErr
	}

	if r.searchDocumentIndexed {
		rows, err := r.searchProjectedProducts(ctx, input)
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return rows, err
	}
	text := func(path string) string {
		document := "CASE WHEN JSON_VALID(payload) THEN payload ELSE '{}' END"
		if r.db.Dialector.Name() == "mysql" {
			return "JSON_UNQUOTE(JSON_EXTRACT(" + document + ", '" + path + "'))"
		}
		return "json_extract(" + document + ", '" + path + "')"
	}
	query := r.db.WithContext(ctx).Model(&domain.Record{}).
		Where("kind = ? AND parent_id = ? AND status = ?", constants.CatalogKindSku, "", constants.CatalogStatusActive).
		Where("JSON_VALID(payload)").
		Where("COALESCE(CAST(" + text("$.catalog_visible") + " AS CHAR),'true') NOT IN ('false','0')")
	if input.Category != "" {
		if r.searchCategoryIndexed {
			query = query.Where("search_category_v1 = ?", input.Category)
		} else {
			query = query.Where(text("$.category")+" = ?", input.Category)
		}
	}
	if input.Brand != "" {
		query = query.Where(text("$.brand")+" = ?", input.Brand)
	}
	if input.Query != "" {
		value := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(input.Query))
		conditions, args := []string{}, []any{}
		for _, path := range []string{"$.name", "$.brand", "$.category"} {
			conditions = append(conditions, "LOWER(COALESCE("+text(path)+",'')) LIKE ? ESCAPE '!'")
			args = append(args, "%"+value+"%")
		}
		query = query.Where("("+strings.Join(conditions, " OR ")+")", args...)
	}
	number := func(path string) string {
		kind := "INTEGER"
		if r.db.Dialector.Name() == "mysql" {
			kind = "SIGNED"
		}
		return "CAST(COALESCE(" + text(path) + ",0) AS " + kind + ")"
	}
	rank := "0"
	switch input.Sort {
	case constants.CatalogSortPriceAsc:
		rank = number("$.price_cents")
		query = query.Order(number("$.price_cents") + " ASC")
	case constants.CatalogSortPriceDesc:
		rank = number("$.price_cents")
		query = query.Order(number("$.price_cents") + " DESC")
	case constants.CatalogSortSales, constants.CatalogSortSalesDesc:
		rank = number("$.sales")
		query = query.Order(number("$.sales") + " DESC")
	}
	if condition, args := searchAfter(rank, input.Sort == constants.CatalogSortPriceAsc, position); condition != "" {
		query = query.Where(condition, args...)
	}
	query = query.Select("catalog_records.*, " + rank + " AS search_sort_value")
	rows := []domain.Record{}
	err := query.Order("created_at DESC,id DESC").Limit(input.Limit).Find(&rows).Error
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	return rows, err
}
