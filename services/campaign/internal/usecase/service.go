package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/campaign/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct{ Repo *repository.Repository }

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "usecase.campaign."+c.Action)
	defer sp.End()
	switch c.Action {
	case "list":
		return s.Repo.List(ctx)
	case "evaluate":
		return s.Repo.Evaluate(ctx, c)
	}
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != "system" && c.Role != "admin" {
		return nil, status.Error(codes.PermissionDenied, "internal campaign command required")
	}
	switch c.Action {
	case "reserve":
		return s.Repo.Reserve(ctx, c)
	case "confirm", "cancel":
		return s.Repo.Transition(ctx, c)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
