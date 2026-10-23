package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct{ Repo *repository.Repository }

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "usecase.coupon."+c.Action)
	defer sp.End()
	if c.Action == "templates" {
		return s.Repo.Templates(ctx, c)
	}
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Action == "list" {
		return s.Repo.List(ctx, c)
	}
	if c.Action == "claim" {
		return s.Repo.Claim(ctx, c)
	}
	if c.Role != "system" && c.Role != "admin" {
		return nil, status.Error(codes.PermissionDenied, "internal coupon command required")
	}
	switch c.Action {
	case "reserve":
		return s.Repo.Reserve(ctx, c)
	case "confirm", "cancel":
		return s.Repo.Transition(ctx, c)
	case "refund":
		return s.Repo.Refund(ctx, c)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type != "RefundSucceeded" {
		return nil
	}
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}
	_, err := s.Execute(ctx, platform.Command{Action: "refund", ID: e.AggregateID, UserID: e.UserID, Role: "system", Data: data})
	return err
}
