package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/search/internal/repository"
	"strings"
)

func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "search.usecase.PolicyGet")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	p, v, e := s.repo.Policy(ctx)
	return platform.Result{"config": p, "version": v}, e
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "search.usecase.PolicyUpsert")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Config repository.Policy `json:"config"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	p := in.Config
	switch p.DefaultSort {
	case "", constants.CatalogSortPriceAsc, constants.CatalogSortPriceDesc, constants.CatalogSortSales, constants.CatalogSortSalesDesc, constants.CatalogSortNewest:
	default:
		return nil, bad("invalid default search order")
	}
	if p.MaxResults < 1 || p.MaxResults > 100 || len(p.ExcludedIDs) > 500 || len(p.HiddenBrands) > 100 {
		return nil, bad("invalid search result window or exclusions")
	}
	for _, list := range [][]string{p.ExcludedIDs, p.HiddenBrands} {
		seen := map[string]bool{}
		for _, v := range list {
			if strings.TrimSpace(v) == "" || len([]rune(v)) > 150 || seen[v] {
				return nil, bad("invalid or duplicate exclusion")
			}
			seen[v] = true
		}
	}
	return s.repo.PolicyUpsert(ctx, c, p)
}
