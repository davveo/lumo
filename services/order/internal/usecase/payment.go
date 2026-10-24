package usecase

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func (s *Service) AcceptPayment(ctx context.Context, request AcceptPaymentRequest) (traceResult AcceptPaymentResult, traceErr error) {
	ctx, span := platform.Start(ctx, "order.usecase.accept_payment")
	defer span.End()
	platform.TraceRequest(ctx, request)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := requireInternalUser(request.Actor); err != nil {
		return AcceptPaymentResult{}, err
	}
	var result AcceptPaymentResult
	err := s.r.Transaction(ctx, func(tx *repository.Tx) error {
		order, err := tx.Get(request.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return status.Error(codes.NotFound, "order not found")
		}
		if err != nil {
			return err
		}
		if err = own(request.Actor, order); err != nil {
			return err
		}
		if request.PaymentID == "" || request.AmountCents != order.Payable {
			return status.Error(codes.InvalidArgument, "payment does not match order")
		}
		accepted := false
		emit := ""
		if order.PaymentID == request.PaymentID {
			accepted = true
		} else if order.PaymentID == "" && (order.Status == constants.OrderStatusPendingPayment || order.Status == constants.OrderStatusClosing) {
			paidAt := request.PaidAt
			if paidAt.IsZero() {
				paidAt = time.Now().UTC()
			}
			if !paidAt.After(order.ExpiresAt) {
				order.PaymentID = request.PaymentID
				order.Status = constants.OrderStatusPaidConfirming
				accepted = true
				emit = constants.EventOrderPaymentAccepted
			}
		}
		if !accepted {
			emit = constants.EventPaymentRejected
			if order.PaymentID == "" && (order.Status == constants.OrderStatusPendingPayment || order.Status == constants.OrderStatusClosing) {
				order.Status = constants.OrderStatusClosed
				if err = tx.Emit(platform.Event{ID: "OrderClosed-" + order.ID, Type: constants.EventOrderClosed, AggregateID: order.ID, UserID: order.UserID, Data: map[string]any{"order_id": order.ID, "reason": "payment_after_deadline"}}); err != nil {
					return err
				}
			}
		}
		result = AcceptPaymentResult{Accepted: accepted, OrderID: order.ID, Status: order.Status, PaymentID: request.PaymentID}
		if err = tx.Save(&order); err != nil {
			return err
		}
		if emit != "" {
			return tx.Emit(platform.Event{ID: emit + "-" + request.PaymentID, Type: emit, AggregateID: order.ID, UserID: order.UserID, Data: map[string]any{"order_id": order.ID, "payment_id": request.PaymentID, "amount_cents": request.AmountCents}})
		}
		return nil
	})
	return result, err
}
