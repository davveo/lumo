package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) SkuSales(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "analytics.usecase.SkuSales")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		IDs  []string `json:"ids"`
		Days int      `json:"days"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if in.Days == 0 {
		in.Days = 30
	}
	if len(in.IDs) < 1 || len(in.IDs) > 100 || in.Days < 7 || in.Days > 90 {
		return nil, bad("invalid sales window")
	}
	seen := map[string]bool{}
	for _, id := range in.IDs {
		if id == "" || len(id) > 128 || seen[id] {
			return nil, bad("invalid sku identifiers")
		}
		seen[id] = true
	}
	return s.repo.SkuSales(ctx, in.IDs, in.Days)
}
