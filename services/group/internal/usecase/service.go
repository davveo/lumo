package usecase

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/group/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	Repo *repository.Repository
	RPC  platform.Caller
}

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "group.usecase.List")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	result, e := s.Repo.List(ctx)
	if e != nil {
		return nil, e
	}
	rules, e := s.Repo.PublicRules(ctx)
	if e != nil {
		return nil, e
	}
	result["rules"] = rules
	return result, nil
}
func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "group.usecase.Get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	return s.Repo.Get(ctx, c.ID)
}
func (s *Service) Create(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "group.usecase.Create")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if prior, found, err := s.Repo.ExistingCreate(ctx, c); err != nil || found {
		return prior, err
	}
	if s.RPC != nil {
		_, e := platform.NewClients(
			s.RPC,
		).Catalog.Get(
			ctx,
			platform.Request{
				ID:     platform.Text(c.Data, "sku_id"),
				UserID: c.UserID,
				Role:   c.Role,
			},
		)
		if e != nil {
			return nil, e
		}
	}
	request, e := s.applyRule(ctx, c)
	if e != nil {
		return nil, e
	}
	return s.Repo.Create(ctx, request)
}
func (s *Service) Reserve(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "group.usecase.Reserve")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal group command required")
	}
	return s.Repo.Reserve(ctx, c)
}
func (s *Service) Paid(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "group.usecase.Paid")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal group command required")
	}
	return s.Repo.Paid(ctx, c)
}
func (s *Service) Cancel(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "group.usecase.Cancel")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal group command required")
	}
	return s.Repo.Cancel(ctx, c)
}
func (s *Service) Expire(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "group.usecase.Expire")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal group command required")
	}
	return s.Repo.Expire(ctx)
}

func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type != constants.EventOrderPaymentAccepted {
		return nil
	}
	exists, er := s.Repo.HasSeat(ctx, e.AggregateID)
	if er != nil || !exists {
		return er
	}
	_, er = s.Paid(
		ctx,
		platform.Request{
			ID:     e.AggregateID,
			UserID: e.UserID,
			Role:   constants.RoleSystem,
		},
	)
	return er
}
func (s *Service) Tick(ctx context.Context) error {
	_, e := s.Expire(ctx, platform.Request{
		UserID: constants.RoleSystem,
		Role:   constants.RoleSystem,
	})
	return e
}
