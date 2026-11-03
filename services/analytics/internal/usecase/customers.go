package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) CustomerStats(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "analytics.usecase.CustomerStats")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.repo.CustomerStats(ctx, c)
}
