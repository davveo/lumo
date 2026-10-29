package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) AdminList(ctx context.Context, c platform.Request) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "iam.usecase.AdminList")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err = platform.RequireAdmin(c); err != nil {
		return nil, err
	}
	return s.repo.AdminList(ctx, c)
}
func (s *Service) AdminSummary(ctx context.Context, c platform.Request) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "iam.usecase.AdminSummary")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err = platform.RequireAdmin(c); err != nil {
		return nil, err
	}
	return s.repo.AdminSummary(ctx, c)
}
func (s *Service) SetActive(ctx context.Context, c platform.Request) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "iam.usecase.SetActive")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err = platform.RequireAdmin(c); err != nil {
		return nil, err
	}
	return s.repo.SetActive(ctx, c)
}

func (s *Service) AuditList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "iam.usecase.AuditList")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.repo.AuditList(ctx, c)
}
func (s *Service) AdminGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "iam.usecase.AdminGet")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.repo.AdminGet(ctx, c)
}
