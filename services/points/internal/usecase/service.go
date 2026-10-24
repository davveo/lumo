package usecase

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct{ Repo *repository.Repository }

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.points."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	switch c.Action {
	case constants.ActionGet:
		return s.Repo.Get(ctx, c)
	case constants.ActionCheckin:
		return s.Repo.Credit(ctx, c, true)
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal points command required")
	}
	switch c.Action {
	case constants.ActionReserve:
		return s.Repo.Reserve(ctx, c)
	case constants.ActionConfirm, constants.ActionCancel:
		return s.Repo.Transition(ctx, c)
	case constants.ActionRefund:
		return s.Repo.Refund(ctx, c)
	case constants.ActionReward:
		return s.Repo.Credit(ctx, c, false)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type == constants.EventRefundSucceeded {
		_, err := s.Execute(
			ctx,
			platform.Command{
				Action: constants.ActionRefund,
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
		if amount == 0 {
			amount = platform.Int(e.Data, "payable_cents") / 100
		}
		_, err := s.Execute(
			ctx,
			platform.Command{
				Action: constants.ActionReward,
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
