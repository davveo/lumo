package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) AdminList(ctx context.Context, c platform.Request) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "refund.usecase.AdminList")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err = platform.RequireAdmin(c); err != nil {
		return nil, err
	}
	return s.r.AdminList(ctx, c)
}
func (s *Service) AdminSummary(ctx context.Context, c platform.Request) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "refund.usecase.AdminSummary")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err = platform.RequireAdmin(c); err != nil {
		return nil, err
	}
	return s.r.AdminSummary(ctx, c)
}
