package usecase

import (
	"context"

	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) CloseBegin(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, "close_begin", request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.Status == "CLOSED" || order.Status == "CREATE_FAILED" {
			return transition{unchanged: true}, nil
		}
		if order.Status != "PENDING_PAYMENT" && order.Status != "CLOSING" {
			return transition{}, status.Error(codes.FailedPrecondition, "only unpaid orders can close")
		}
		order.Status = "CLOSING"
		return transition{}, nil
	})
}
func (s *Service) CloseComplete(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, "close_complete", request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.Status == "CLOSED" {
			return transition{unchanged: true}, nil
		}
		if order.Status != "CLOSING" {
			return transition{}, status.Error(codes.FailedPrecondition, "close was not begun")
		}
		order.Status = "CLOSED"
		return transition{eventType: "OrderClosed"}, nil
	})
}
