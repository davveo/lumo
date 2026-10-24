package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"github.com/davveo/lumo/services/inventory/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

type Service struct{ Repo *repository.Repository }

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.inventory."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Action == "get" {
		return s.Repo.Get(ctx, c.ID)
	}
	if c.Action == "batch_get" {
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
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != "admin" && c.Role != "system" {
		return nil, status.Error(codes.PermissionDenied, "internal inventory command required")
	}
	switch c.Action {
	case "reserve":
		var v struct {
			OrderID string        `json:"order_id"`
			Lines   []domain.Line `json:"lines"`
		}
		if e := platform.Decode(c.Data, &v); e != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid reservation")
		}
		return s.Repo.Reserve(ctx, c, v.OrderID, v.Lines)
	case "confirm", "cancel", "dispatch":
		return s.Repo.Transition(ctx, c)
	case "restock":
		return s.Repo.Restock(ctx, c)
	case "adjust":
		if e := platform.RequireAdmin(c); e != nil {
			return nil, e
		}
		return s.Repo.Adjust(ctx, c)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
