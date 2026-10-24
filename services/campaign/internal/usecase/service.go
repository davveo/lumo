package usecase

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/campaign/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct{ Repo *repository.Repository }

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.campaign."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	switch c.Action {
	case constants.ActionList:
		return s.Repo.List(ctx)
	case constants.ActionEvaluate:
		return s.Repo.Evaluate(ctx, c)
	}
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal campaign command required")
	}
	switch c.Action {
	case constants.ActionReserve:
		return s.Repo.Reserve(ctx, c)
	case constants.ActionConfirm, constants.ActionCancel:
		return s.Repo.Transition(ctx, c)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
