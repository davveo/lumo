package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) AdminList(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "inventory.usecase.AdminList")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.AdminList(ctx, c)
}
func (s *Service) Threshold(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "inventory.usecase.Threshold")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.Threshold(ctx, c)
}
func (s *Service) Ledger(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "inventory.usecase.Ledger")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.Ledger(ctx, c)
}
