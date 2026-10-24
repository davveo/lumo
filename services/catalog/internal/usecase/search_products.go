package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) SearchProducts(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "catalog.usecase."+constants.ActionSearchProducts)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal product search required")
	}
	var input struct {
		Query    string `json:"query"`
		Category string `json:"category"`
		Brand    string `json:"brand"`
		Sort     string `json:"sort"`
		Limit    int    `json:"limit"`
	}
	b, err := json.Marshal(c.Data)
	if err != nil {
		return nil, bad("invalid product search")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		return nil, bad("invalid product search")
	}
	for _, field := range []struct {
		value *string
		max   int
	}{{&input.Query, 120}, {&input.Category, 80}, {&input.Brand, 80}} {
		*field.value = strings.TrimSpace(*field.value)
		if !utf8.ValidString(*field.value) || utf8.RuneCountInString(*field.value) > field.max ||
			strings.IndexFunc(*field.value, unicode.IsControl) >= 0 {
			return nil, bad("invalid product search text")
		}
	}
	if input.Limit == 0 {
		input.Limit = 100
	}
	if input.Limit < 1 || input.Limit > 100 {
		return nil, bad("product search limit must be between 1 and 100")
	}
	switch input.Sort {
	case constants.CatalogSortDefault,
		constants.CatalogSortPriceAsc,
		constants.CatalogSortPriceDesc,
		constants.CatalogSortSales,
		constants.CatalogSortSalesDesc,
		constants.CatalogSortNewest:
	default:
		return nil, bad("invalid product search sort")
	}
	rows, err := s.repo.SearchProducts(
		ctx,
		repository.ProductSearch{
			Query:    input.Query,
			Category: input.Category,
			Brand:    input.Brand,
			Sort:     input.Sort,
			Limit:    input.Limit,
		},
	)
	if err != nil {
		return nil, err
	}
	items := make([]platform.Result, 0, len(rows))
	for _, row := range rows {
		items = append(items, view(row))
	}
	return platform.Result{"items": items, "limit": input.Limit}, nil
}
