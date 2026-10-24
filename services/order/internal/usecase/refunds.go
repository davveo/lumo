package usecase

import (
	"context"
	"errors"

	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func (s *Service) Refunded(ctx context.Context, request RecordRefundRequest) (domain.Order, error) {
	return s.update(ctx, "refunded", request.OrderRequest, internalAccess, func(tx *repository.Tx, order *domain.Order) (transition, error) {
		if request.RefundID == "" || request.AmountCents <= 0 {
			return transition{}, status.Error(codes.InvalidArgument, "refund receipt required")
		}
		if _, err := tx.Receipt(request.RefundID); err == nil {
			return transition{unchanged: true}, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return transition{}, err
		}
		if order.Refunded+request.AmountCents > order.Payable {
			return transition{}, status.Error(codes.FailedPrecondition, "refund exceeds captured amount")
		}
		order.Refunded += request.AmountCents
		if err := tx.AddReceipt(domain.RefundReceipt{ID: request.RefundID, OrderID: order.ID, Amount: request.AmountCents}); err != nil {
			return transition{}, err
		}
		return transition{}, nil
	}, request)
}
