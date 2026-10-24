package usecase

import (
	"context"
	"encoding/json"

	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Ready(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, "ready", request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.GroupFailed {
			return transition{}, status.Error(codes.FailedPrecondition, "group already failed")
		}
		if order.Status == "READY_TO_FULFILL" || order.Status == "SHIPPED" || order.Status == "COMPLETED" {
			return transition{unchanged: true}, nil
		}
		if order.Status != "PAID_CONFIRMING" && order.Status != "PAID_WAIT_GROUP" {
			return transition{}, status.Error(codes.FailedPrecondition, "payment not accepted")
		}
		order.ResourcesCommitted = true
		if order.GroupID != "" && !order.GroupFormed {
			order.Status = "PAID_WAIT_GROUP"
			return transition{}, nil
		}
		order.Status = "READY_TO_FULFILL"
		var quote map[string]any
		_ = json.Unmarshal([]byte(order.Quote), &quote)
		return transition{eventType: "OrderReadyToFulfill", data: map[string]any{"order_id": order.ID, "reservation_id": order.ID, "items": quote["items"]}}, nil
	})
}
func (s *Service) WaitGroup(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, "wait_group", request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.Status == "PAID_CONFIRMING" {
			order.ResourcesCommitted = true
			order.Status = "PAID_WAIT_GROUP"
		}
		return transition{}, nil
	})
}
func (s *Service) Shipped(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, "shipped", request, internalAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.Status == "SHIPPED" || order.Status == "COMPLETED" {
			return transition{unchanged: true}, nil
		}
		if order.Status != "READY_TO_FULFILL" {
			return transition{}, status.Error(codes.FailedPrecondition, "order not ready to ship")
		}
		order.Status = "SHIPPED"
		return transition{}, nil
	})
}
func (s *Service) Complete(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(ctx, "complete", request, memberAccess, func(_ *repository.Tx, order *domain.Order) (transition, error) {
		if order.Status == "COMPLETED" {
			return transition{unchanged: true}, nil
		}
		if order.Status != "SHIPPED" {
			return transition{}, status.Error(codes.FailedPrecondition, "order not shipped")
		}
		order.Status = "COMPLETED"
		return transition{eventType: "OrderCompleted", data: map[string]any{"order_id": order.ID, "payable_cents": order.Payable}}, nil
	})
}
