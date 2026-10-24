package usecase

import (
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

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.group."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	switch c.Action {
	case "list":
		return s.Repo.List(ctx)
	case "get":
		return s.Repo.Get(ctx, c.ID)
	}
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Action == "create" {
		if s.RPC != nil {
			_, e := s.RPC.Call(ctx, "catalog", platform.Command{Action: "get", ID: platform.Text(c.Data, "sku_id"), UserID: c.UserID, Role: c.Role})
			if e != nil {
				return nil, e
			}
		}
		return s.Repo.Create(ctx, c)
	}
	if c.Role != "system" && c.Role != "admin" {
		return nil, status.Error(codes.PermissionDenied, "internal group command required")
	}
	switch c.Action {
	case "reserve":
		return s.Repo.Reserve(ctx, c)
	case "paid":
		return s.Repo.Paid(ctx, c)
	case "cancel":
		return s.Repo.Cancel(ctx, c)
	case "expire":
		return s.Repo.Expire(ctx)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type != "OrderPaymentAccepted" {
		return nil
	}
	exists, er := s.Repo.HasSeat(ctx, e.AggregateID)
	if er != nil || !exists {
		return er
	}
	_, er = s.Execute(ctx, platform.Command{Action: "paid", ID: e.AggregateID, UserID: e.UserID, Role: "system"})
	return er
}
func (s *Service) Tick(ctx context.Context) error { _, e := s.Repo.Expire(ctx); return e }
