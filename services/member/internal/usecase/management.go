package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "member.usecase.PolicyGet")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	p, e := s.repo.Policy(ctx)
	return platform.Result{"config": p, "version": p.Version}, e
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "member.usecase.PolicyUpsert")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Config commercepolicy.Membership `json:"config"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if in.Config.GoldGrowth < 1 || in.Config.GoldGrowth > 1000000000 || in.Config.GrowthSpendCents < 1 || in.Config.GrowthSpendCents > 1000000000 {
		return nil, bad("invalid membership growth rule")
	}
	return s.repo.PolicyUpsert(ctx, c, in.Config)
}
