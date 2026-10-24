package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Ready(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, constants.ActionReady, request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.GroupFailed {
			return transition{}, status.Error(codes.FailedPrecondition, "group already failed")
		}
		if order.Status == constants.OrderStatusReadyToFulfill || order.Status == constants.OrderStatusShipped || order.Status == constants.OrderStatusCompleted {
			return transition{unchanged: true}, nil
		}
		if order.Status != constants.OrderStatusPaidConfirming && order.Status != constants.OrderStatusPaidWaitGroup {
			return transition{}, status.Error(codes.FailedPrecondition, "payment not accepted")
		}
		order.ResourcesCommitted = true
		if order.GroupID != "" && !order.GroupFormed {
			order.Status = constants.OrderStatusPaidWaitGroup
			return transition{}, nil
		}
		order.Status = constants.OrderStatusReadyToFulfill
		var quote map[string]any
		_ = json.Unmarshal([]byte(order.Quote), &quote)
		return transition{eventType: constants.EventOrderReadyToFulfill, data: map[string]any{"order_id": order.ID, "reservation_id": order.ID, "items": quote["items"]}}, nil
	})
}
func (s *Service) WaitGroup(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, constants.ActionWaitGroup, request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.Status == constants.OrderStatusPaidConfirming {
			order.ResourcesCommitted = true
			order.Status = constants.OrderStatusPaidWaitGroup
		}
		return transition{}, nil
	})
}
func (s *Service) Shipped(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, constants.ActionShipped, request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.Status == constants.OrderStatusShipped || order.Status == constants.OrderStatusCompleted {
			return transition{unchanged: true}, nil
		}
		if order.Status != constants.OrderStatusReadyToFulfill {
			return transition{}, status.Error(codes.FailedPrecondition, "order not ready to ship")
		}
		order.Status = constants.OrderStatusShipped
		return transition{}, nil
	})
}
func (s *Service) Complete(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, constants.ActionComplete, request, memberAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.Status == constants.OrderStatusCompleted {
			return transition{unchanged: true}, nil
		}
		if order.Status != constants.OrderStatusShipped {
			return transition{}, status.Error(codes.FailedPrecondition, "order not shipped")
		}
		order.Status = constants.OrderStatusCompleted
		return transition{eventType: constants.EventOrderCompleted, data: map[string]any{"order_id": order.ID, "payable_cents": order.Payable}}, nil
	})
}
