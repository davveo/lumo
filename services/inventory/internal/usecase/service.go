package usecase

import (
	"context"
	"strings"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"github.com/davveo/lumo/services/inventory/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct{ Repo *repository.Repository }

func (s *Service) Adjust(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.inventory.adjust")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal inventory command required")
	}

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.Adjust(ctx, c)
}

func (s *Service) BatchGet(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.inventory.batch_get")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	var input struct {
		IDs []string `json:"ids"`
	}
	if err := platform.Decode(c.Data, &input); err != nil || len(input.IDs) == 0 || len(input.IDs) > 500 {
		return nil, status.Error(codes.InvalidArgument, "ids must contain between 1 and 500 SKU IDs")
	}
	ids, seen := []string{}, map[string]bool{}
	for _, id := range input.IDs {
		if id == "" || len(id) > 150 || strings.TrimSpace(id) != id {
			return nil, status.Error(codes.InvalidArgument, "invalid SKU ID")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return s.Repo.GetMany(ctx, ids)
}

func (s *Service) Cancel(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.inventory.cancel")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal inventory command required")
	}

	return s.Repo.Cancel(ctx, c)
}

func (s *Service) Confirm(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.inventory.confirm")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal inventory command required")
	}

	return s.Repo.Confirm(ctx, c)
}

func (s *Service) Dispatch(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.inventory.dispatch")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal inventory command required")
	}

	return s.Repo.Dispatch(ctx, c)
}

func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.inventory.get")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	return s.Repo.Get(ctx, c.ID)
}

func (s *Service) Reserve(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.inventory.reserve")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal inventory command required")
	}

	var v struct {
		OrderID string        `json:"order_id"`
		Lines   []domain.Line `json:"lines"`
	}
	if e := platform.Decode(c.Data, &v); e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid reservation")
	}
	return s.Repo.Reserve(ctx, c, v.OrderID, v.Lines)
}

func (s *Service) Restock(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.inventory.restock")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal inventory command required")
	}

	return s.Repo.Restock(ctx, c)
}
