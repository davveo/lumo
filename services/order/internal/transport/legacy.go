package transport

import (
	"context"
	"encoding/json"
	"time"

	"github.com/davveo/lumo/internal/orderwire"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/usecase"
)

func legacyActor(command platform.Command) usecase.Actor {
	return usecase.Actor{
		UserID: command.UserID,
		Role:   platform.StaffRoleAtBoundary(command.Role, "order", command.Action),
	}
}
func (a legacyAdapter) batchGet(ctx context.Context, c platform.Command) (platform.Result, error) {
	var input struct {
		IDs []string `json:"ids"`
	}
	if err := platform.Decode(c.Data, &input); err != nil {
		return nil, err
	}
	rows, err := a.service.BatchGet(ctx, legacyActor(c), input.IDs)
	if err != nil {
		return nil, err
	}
	items := []platform.Result{}
	for _, row := range rows {
		items = append(items, orderwire.Result(orderMessage(row)))
	}
	return platform.Result{"items": items}, nil
}
func legacyRequest(command platform.Command) usecase.OrderRequest {
	return usecase.OrderRequest{Actor: legacyActor(command), ID: command.ID}
}
func legacyTime(raw string) time.Time {
	value, _ := time.Parse(time.RFC3339Nano, raw)
	return value
}
func legacyOrder(call func(context.Context, usecase.OrderRequest) (domain.Order, error)) legacyAction {
	return func(ctx context.Context, command platform.Command) (platform.Result, error) {
		order, err := call(ctx, legacyRequest(command))
		if err != nil {
			return nil, err
		}
		return orderwire.Result(orderMessage(order)), nil
	}
}
func (a legacyAdapter) list(ctx context.Context, command platform.Command) (platform.Result, error) {
	orders, page, err := a.service.ListPage(ctx, legacyActor(command), command.Data)
	if err != nil {
		return nil, err
	}
	items := make([]platform.Result, 0, len(orders))
	for _, order := range orders {
		items = append(items, orderwire.Result(orderMessage(order)))
	}
	page["items"] = items
	return page, nil
}
func (a legacyAdapter) create(ctx context.Context, command platform.Command) (platform.Result, error) {
	quote, _ := json.Marshal(command.Data["quote"])
	address, _ := json.Marshal(command.Data["address"])
	order, err := a.service.Create(ctx, usecase.CreateOrderRequest{OrderRequest: legacyRequest(command),
		Quote: quote, Address: address, Note: platform.Text(command.Data, "note"),
		Source: platform.Text(
			command.Data,
			"source",
		), ExpiresAt: legacyTime(platform.Text(command.Data, "expires_at"))})
	if err != nil {
		return nil, err
	}
	return orderwire.Result(orderMessage(order)), nil
}
func (a legacyAdapter) acceptPayment(ctx context.Context, command platform.Command) (platform.Result, error) {
	result, err := a.service.AcceptPayment(ctx, usecase.AcceptPaymentRequest{OrderRequest: legacyRequest(command),
		PaymentID: platform.Text(command.Data, "payment_id"), AmountCents: platform.Int(command.Data, "amount_cents"),
		PaidAt: legacyTime(platform.Text(command.Data, "paid_at"))})
	if err != nil {
		return nil, err
	}
	return platform.Result{
		"accepted":   result.Accepted,
		"order_id":   result.OrderID,
		"status":     result.Status,
		"payment_id": result.PaymentID,
	}, nil
}
func (a legacyAdapter) refunded(ctx context.Context, command platform.Command) (platform.Result, error) {
	order, err := a.service.Refunded(ctx, usecase.RecordRefundRequest{OrderRequest: legacyRequest(command),
		RefundID: platform.Text(command.Data, "refund_id"), AmountCents: platform.Int(command.Data, "amount_cents")})
	if err != nil {
		return nil, err
	}
	return orderwire.Result(orderMessage(order)), nil
}

func (a legacyAdapter) changeAddress(ctx context.Context, c platform.Command) (platform.Result, error) {
	raw, _ := json.Marshal(c.Data["address"])
	o, e := a.service.ChangeAddress(
		ctx,
		usecase.ChangeAddressRequest{
			OrderRequest:    legacyRequest(c),
			ExpectedVersion: platform.Int(c.Data, "expected_version"),
			Address:         raw,
			ShippingCents:   platform.Int(c.Data, "shipping_cents"),
		},
	)
	if e != nil {
		return nil, e
	}
	return orderwire.Result(orderMessage(o)), nil
}
