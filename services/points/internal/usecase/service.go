package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct{ Repo *repository.Repository }

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "usecase.points."+c.Action)
	defer sp.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	switch c.Action {
	case "get":
		return s.Repo.Get(ctx, c)
	case "checkin":
		return s.Repo.Credit(ctx, c, true)
	}
	if c.Role != "system" && c.Role != "admin" {
		return nil, status.Error(codes.PermissionDenied, "internal points command required")
	}
	switch c.Action {
	case "reserve":
		return s.Repo.Reserve(ctx, c)
	case "confirm", "cancel":
		return s.Repo.Transition(ctx, c)
	case "refund":
		return s.Repo.Refund(ctx, c)
	case "reward":
		return s.Repo.Credit(ctx, c, false)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type == "RefundSucceeded" {
		_, err := s.Execute(ctx, platform.Command{Action: "refund", ID: e.AggregateID, UserID: e.UserID, Role: "system", Data: e.Data})
		return err
	}
	if e.Type == "OrderCompleted" {
		amount := platform.Int(e.Data, "reward_points")
		if amount == 0 {
			amount = platform.Int(e.Data, "payable_cents") / 100
		}
		_, err := s.Execute(ctx, platform.Command{Action: "reward", ID: e.AggregateID, UserID: e.UserID, Role: "system", Data: map[string]any{"amount": amount}})
		return err
	}
	return nil
}
