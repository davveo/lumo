package usecase

import (
	"context"

	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) GroupFormed(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, "group_formed", request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.GroupFailed {
			return transition{}, status.Error(codes.FailedPrecondition, "group already failed")
		}
		order.GroupFormed = true
		return transition{}, nil
	})
}
func (s *Service) GroupFailed(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, "group_failed", request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.Status == "SHIPPED" || order.Status == "COMPLETED" {
			return transition{}, status.Error(codes.FailedPrecondition, "cannot fail dispatched group order")
		}
		order.GroupFailed = true
		order.Status = "TERMINATED"
		return transition{
			eventType: "GroupOrderTerminated",
			data:      map[string]any{"order_id": order.ID, "payment_id": order.PaymentID, "amount_cents": order.Payable}}, nil
	})
}
