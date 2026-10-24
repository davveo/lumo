package usecase

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"errors"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func (s *Service) List(ctx context.Context, actor Actor) (traceResult []domain.Order, traceErr error) {
	ctx, span := platform.Start(ctx, "order.usecase.list")
	defer span.End()
	platform.TraceRequest(ctx, actor)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := requireUser(actor); err != nil {
		return nil, err
	}
	return s.r.List(ctx, actor.UserID, actor.Role == constants.RoleAdmin || actor.Role == constants.RoleSystem)
}
func (s *Service) Get(ctx context.Context, request OrderRequest) (traceResult domain.Order, traceErr error) {
	ctx, span := platform.Start(ctx, "order.usecase.get")
	defer span.End()
	platform.TraceRequest(ctx, request)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := requireUser(request.Actor); err != nil {
		return domain.Order{}, err
	}
	order, err := s.r.Get(ctx, request.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Order{}, status.Error(codes.NotFound, "order not found")
	}
	if err != nil {
		return domain.Order{}, err
	}
	if err = own(request.Actor, order); err != nil {
		return domain.Order{}, err
	}
	return order, nil
}
