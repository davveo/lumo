package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"time"

	"github.com/davveo/lumo/internal/platform"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Topics() []string {
	return []string{constants.EventPaymentSucceeded, constants.EventRefundSucceeded, constants.EventShipmentDispatched, constants.EventDeliveryConfirmed, constants.EventGroupFormed, constants.EventGroupFailed}
}
func eventTime(data map[string]any, key string) time.Time {
	value, _ := time.Parse(time.RFC3339Nano, platform.Text(data, key))
	return value
}
func (s *Service) OnEvent(ctx context.Context, event platform.Event) error {
	request := OrderRequest{Actor: Actor{UserID: event.UserID, Role: constants.RoleSystem}, ID: platform.Text(event.Data, "order_id")}
	switch event.Type {
	case constants.EventPaymentSucceeded:
		_, err := s.AcceptPayment(ctx, AcceptPaymentRequest{
			OrderRequest: request,
			PaymentID:    platform.Text(event.Data, "payment_id"),
			AmountCents:  platform.Int(event.Data, "amount_cents"),
			PaidAt:       eventTime(event.Data, "paid_at"),
		})
		return err
	case constants.EventRefundSucceeded:
		_, err := s.Refunded(ctx, RecordRefundRequest{
			OrderRequest: request,
			RefundID:     platform.Text(event.Data, "refund_id"),
			AmountCents:  platform.Int(event.Data, "amount_cents"),
		})
		return err
	case constants.EventShipmentDispatched:
		if platform.Text(event.Data, "kind") == constants.FulfillmentKindExchange {
			return nil
		}
		_, err := s.Shipped(ctx, request)
		return err
	case constants.EventDeliveryConfirmed:
		if platform.Text(event.Data, "kind") == constants.FulfillmentKindExchange {
			return nil
		}
		_, err := s.Complete(ctx, request)
		return err
	case constants.EventGroupFormed, constants.EventGroupFailed:
		return s.onGroupEvent(ctx, event)
	}
	return nil
}
func (s *Service) onGroupEvent(ctx context.Context, event platform.Event) error {
	var ids []string
	encoded, _ := json.Marshal(event.Data["order_ids"])
	_ = json.Unmarshal(encoded, &ids)
	if id := platform.Text(event.Data, "order_id"); id != "" {
		ids = append(ids, id)
	}
	for _, id := range ids {
		order, err := s.r.Get(ctx, id)
		if err != nil {
			return err
		}
		request := OrderRequest{Actor: Actor{UserID: order.UserID, Role: constants.RoleSystem}, ID: id}
		if event.Type == constants.EventGroupFailed {
			_, err = s.GroupFailed(ctx, request)
		} else {
			_, err = s.GroupFormed(ctx, request)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Tick(ctx context.Context) error {
	rows, err := s.r.Expired(ctx)
	if err != nil {
		return err
	}
	for _, order := range rows {
		_, err = s.rpc.Call(ctx, constants.ServiceCheckout, platform.Command{Action: constants.ActionCancel, ID: order.ID, UserID: order.UserID, Role: constants.RoleSystem})
		if err != nil && status.Code(err) != codes.FailedPrecondition {
			return err
		}
	}
	return nil
}
