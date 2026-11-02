package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

func (s *Service) Dashboard(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	days := int(platform.Int(c.Data, "days"))
	if days == 0 {
		days = 7
	}
	if days != 7 && days != 30 && days != 90 {
		return nil, bad("days must be 7, 30 or 90")
	}
	return s.repo.Metrics(ctx, days)
}
func (s *Service) projectTransaction(ctx context.Context, e platform.Event) error {
	if e.Type != constants.EventPaymentSucceeded && e.Type != constants.EventRefundSucceeded {
		return nil
	}
	orderID := platform.Text(e.Data, "order_id")
	if orderID == "" {
		orderID = e.AggregateID
	}
	kind := "paid"
	key := "payment:" + orderID
	if e.Type == constants.EventRefundSucceeded {
		kind = "refund"
		key = "refund:" + e.AggregateID
	}
	occurred, _ := time.Parse(time.RFC3339Nano, platform.Text(e.Data, "occurred_at"))
	if occurred.IsZero() {
		occurred, _ = time.Parse(time.RFC3339Nano, platform.Text(e.Data, "paid_at"))
	}
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	items := e.Data["items"]
	if items == nil && e.Type == constants.EventPaymentSucceeded && s.rpc != nil {
		order, err := platform.NewClients(
			s.rpc,
		).Order.Get(
			ctx,
			platform.Request{ID: orderID, UserID: e.UserID, Role: constants.RoleSystem},
		)
		if err == nil {
			items = order["items"]
		} else if status.Code(err) != codes.NotFound {
			return err
		}
	}
	if items == nil {
		items = []any{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return s.repo.TransactionFact(
		ctx,
		domain.TransactionFact{
			ID:          key,
			OrderID:     orderID,
			UserID:      e.UserID,
			Kind:        kind,
			AmountCents: platform.Int(e.Data, "amount_cents"),
			Items:       string(raw),
			OccurredAt:  occurred,
		},
	)
}
