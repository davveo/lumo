package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) CloseBegin(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(
		ctx,
		constants.ActionCloseBegin,
		request,
		internalAccess,
		func(_ *repository.Tx, order *domain.Order) (transition, error) {
			if order.Status == constants.OrderStatusClosed || order.Status == constants.OrderStatusCreateFailed {
				return transition{unchanged: true}, nil
			}
			if order.Status != constants.OrderStatusPendingPayment && order.Status != constants.OrderStatusClosing {
				return transition{}, status.Error(codes.FailedPrecondition, "only unpaid orders can close")
			}
			order.Status = constants.OrderStatusClosing
			return transition{}, nil
		},
	)
}
func (s *Service) CloseComplete(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(
		ctx,
		constants.ActionCloseComplete,
		request,
		internalAccess,
		func(_ *repository.Tx, order *domain.Order) (transition, error) {
			if order.Status == constants.OrderStatusClosed {
				return transition{unchanged: true}, nil
			}
			if order.Status != constants.OrderStatusClosing {
				return transition{}, status.Error(codes.FailedPrecondition, "close was not begun")
			}
			order.Status = constants.OrderStatusClosed
			return transition{eventType: constants.EventOrderClosed}, nil
		},
	)
}
