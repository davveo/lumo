package usecase

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	Repo *repository.Repository
	RPC  platform.Caller
}

func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "points.usecase.Get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	value, e := s.Repo.Get(ctx, c)
	if e != nil {
		return nil, e
	}
	policy, e := s.Repo.Policy(ctx)
	if e != nil {
		return nil, e
	}
	value["policy"] = policy
	return value, nil
}
func (s *Service) Checkin(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "points.usecase.Checkin")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	return s.Repo.Checkin(ctx, c)
}
func (s *Service) Reserve(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "points.usecase.Reserve")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal points command required")
	}
	return s.Repo.Reserve(ctx, c)
}
func (s *Service) Confirm(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "points.usecase.Confirm")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal points command required")
	}
	return s.Repo.Confirm(ctx, c)
}
func (s *Service) Cancel(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "points.usecase.Cancel")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal points command required")
	}
	return s.Repo.Cancel(ctx, c)
}
func (s *Service) Refund(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "points.usecase.Refund")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal points command required")
	}
	return s.Repo.Refund(ctx, c)
}
func (s *Service) Reward(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "points.usecase.Reward")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal points command required")
	}
	return s.Repo.Reward(ctx, c)
}

func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type == constants.EventRefundSucceeded {
		_, err := s.Refund(
			ctx,
			platform.Request{
				ID:     e.AggregateID,
				UserID: e.UserID,
				Role:   constants.RoleSystem,
				Data:   e.Data,
			},
		)
		return err
	}
	if e.Type == constants.EventOrderCompleted {
		amount := platform.Int(e.Data, "reward_points")
		if _, snapshotted := e.Data["reward_points"]; !snapshotted {
			amount = platform.Int(e.Data, "payable_cents") / 100
		}
		_, err := s.Reward(
			ctx,
			platform.Request{
				ID:     e.AggregateID,
				UserID: e.UserID,
				Role:   constants.RoleSystem,
				Data:   map[string]any{"amount": amount},
			},
		)
		return err
	}
	return nil
}
