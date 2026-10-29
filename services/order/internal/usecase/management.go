package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"
)

type AdminRequest struct {
	Actor Actor
	Data  map[string]any
}

func (s *Service) AdminList(
	ctx context.Context,
	request AdminRequest,
) (rows []domain.Order, total int64, filter adminquery.Filters, err error) {
	ctx, span := platform.Start(ctx, "order.usecase.AdminList")
	defer span.End()
	platform.TraceRequest(ctx, request)
	defer func() { platform.TraceResponse(ctx, platform.Result{"items": rows, "total": total}, err) }()
	if err = platform.RequireAdmin(platform.Request{UserID: request.Actor.UserID, Role: request.Actor.Role}); err != nil {
		return
	}
	return s.r.AdminList(ctx, request.Data)
}
func (s *Service) AdminSummary(ctx context.Context, request AdminRequest) (result platform.Result, err error) {
	ctx, span := platform.Start(ctx, "order.usecase.AdminSummary")
	defer span.End()
	platform.TraceRequest(ctx, request)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err = platform.RequireAdmin(platform.Request{UserID: request.Actor.UserID, Role: request.Actor.Role}); err != nil {
		return nil, err
	}
	return s.r.AdminSummary(ctx, request.Data)
}
