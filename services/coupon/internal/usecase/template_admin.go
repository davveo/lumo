package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) TemplatesAdmin(ctx context.Context, request platform.Request) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.TemplatesAdmin")
	defer span.End()
	platform.TraceRequest(ctx, request)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err := platform.RequireAdmin(request); err != nil {
		return nil, err
	}
	return s.Repo.TemplatesAdmin(ctx, request)
}

func (s *Service) TemplateUpsert(ctx context.Context, request platform.Request) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.TemplateUpsert")
	defer span.End()
	platform.TraceRequest(ctx, request)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err := platform.RequireAdmin(request); err != nil {
		return nil, err
	}
	return s.Repo.TemplateUpsert(ctx, request)
}
