package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/recommendation/internal/repository"
	"math"
	"strings"
)

func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "recommendation.usecase.PolicyGet")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	p, v, e := s.repo.Policy(ctx)
	return platform.Result{"config": p, "version": v}, e
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "recommendation.usecase.PolicyUpsert")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.Data["config"] == nil {
		return nil, bad("recommendation configuration required")
	}
	var in struct {
		Config repository.Policy `json:"config"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	p := in.Config
	for _, v := range []float64{p.FavoriteWeight, p.CompareWeight, p.HistoryWeight, p.PurchaseWeight, p.SearchWeight, p.PopularityWeight, p.FreshnessWeight, p.PersonalWeight, p.SimilarWeight} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
			return nil, bad("recommendation weights must be between 0 and 100")
		}
	}
	if len(p.PinnedIDs) > 20 || len(p.ExcludedIDs) > 500 {
		return nil, bad("at most 20 pins and 500 exclusions")
	}
	for _, list := range [][]string{p.PinnedIDs, p.ExcludedIDs} {
		seen := map[string]bool{}
		for _, id := range list {
			if id == "" || strings.TrimSpace(id) != id || len(id) > 150 || seen[id] {
				return nil, bad("invalid or duplicate product id")
			}
			seen[id] = true
		}
	}
	return s.repo.PolicyUpsert(ctx, c, p)
}
