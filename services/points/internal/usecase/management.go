package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "points.usecase.PolicyGet")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	p, e := s.Repo.Policy(ctx)
	return platform.Result{"config": p, "version": p.Version}, e
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "points.usecase.PolicyUpsert")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.Data["config"] == nil {
		return nil, status.Error(codes.InvalidArgument, "points configuration required")
	}
	var input struct {
		Config commercepolicy.Points `json:"config"`
	}
	if e := platform.Decode(c.Data, &input); e != nil {
		return nil, e
	}
	p := input.Config
	if p.CheckinPoints < 0 || p.CheckinPoints > 10000 || p.DeductionBPS < 0 || p.DeductionBPS > 10000 || p.RewardPerYuan < 0 || p.RewardPerYuan > 100 {
		return nil, status.Error(codes.InvalidArgument, "invalid points or membership rule")
	}
	return s.Repo.PolicyUpsert(ctx, c, p)
}
func (s *Service) AdminGet(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "points.usecase.AdminGet")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "member id required")
	}
	if e := s.requireMember(ctx, c); e != nil {
		return nil, e
	}
	return s.Repo.AdminGet(ctx, c)
}
func (s *Service) Ledger(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "points.usecase.Ledger")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "member id required")
	}
	if e := s.requireMember(ctx, c); e != nil {
		return nil, e
	}
	return s.Repo.Ledger(ctx, c)
}
func (s *Service) Adjust(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "points.usecase.Adjust")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if e := s.requireMember(ctx, c); e != nil {
		return nil, e
	}
	return s.Repo.Adjust(ctx, c)
}

func (s *Service) requireMember(ctx context.Context, c platform.Request) (spanErr0 error) {
	ctx, span := platform.Start(ctx, "points.usecase.requireMember")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	account, e := platform.NewClients(s.RPC).IAM.AdminGet(ctx, platform.Request{ID: c.ID, UserID: c.UserID, Role: c.Role})
	if e != nil {
		return e
	}
	if platform.Text(account, "role") != constants.RoleUser {
		return status.Error(codes.FailedPrecondition, "select an ordinary member account")
	}
	return nil
}
