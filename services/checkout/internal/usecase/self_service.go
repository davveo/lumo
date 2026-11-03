package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
	"time"
)

func (s *Service) ChangeAddress(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	clients := platform.NewClients(s.rpc)
	order, e := clients.Order.Get(ctx, c)
	if e != nil {
		return nil, e
	}
	if platform.Text(order, "user_id") != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "order ownership mismatch")
	}
	if platform.Int(c.Data, "expected_version") != platform.Int(order, "version") {
		return nil, status.Error(codes.Aborted, "订单已变化，请刷新后重试")
	}
	address, e := clients.Address.Get(
		ctx,
		platform.Request{ID: platform.Text(c.Data, "address_id"), UserID: c.UserID, Role: c.Role},
	)
	if e != nil {
		return nil, e
	}
	var snapshot struct {
		Quote platform.Result `json:"quote"`
		Items []struct {
			Quantity int64 `json:"quantity"`
		} `json:"items"`
	}
	if e = platform.Decode(order, &snapshot); e != nil {
		return nil, e
	}
	qty := int64(0)
	for _, line := range snapshot.Items {
		qty += line.Quantity
	}
	shipping, e := clients.Logistics.Quote(
		ctx,
		platform.Request{
			UserID: c.UserID,
			Role:   c.Role,
			Data: map[string]any{
				"address":        address,
				"subtotal_cents": snapshot.Quote["subtotal_cents"],
				"quantity":       qty,
			},
		},
	)
	if e != nil {
		return nil, e
	}
	if shipping["serviceable"] != true {
		return nil, status.Error(codes.FailedPrecondition, "新地址暂不可配送")
	}
	return clients.Order.ChangeAddress(
		ctx,
		platform.Request{
			ID:     c.ID,
			UserID: c.UserID,
			Role:   constants.RoleSystem,
			Data: map[string]any{
				"expected_version": platform.Int(c.Data, "expected_version"),
				"address":          address,
				"shipping_cents":   platform.Int(shipping, "shipping_cents"),
			},
		},
	)
}
func (s *Service) Timeline(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	clients := platform.NewClients(s.rpc)
	o, e := clients.Order.Get(ctx, c)
	if e != nil {
		return nil, e
	}
	if platform.Text(o, "user_id") != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "order ownership mismatch")
	}
	events := []platform.Result{}
	failures := map[string]string{}
	appendEvent := func(kind, id, at, label string) {
		if t, e := time.Parse(time.RFC3339Nano, at); e == nil && !t.IsZero() {
			events = append(
				events,
				platform.Result{"kind": kind, "id": id, "at": t.UTC().Format(time.RFC3339Nano), "status": label},
			)
		}
	}
	appendEvent("order", c.ID, platform.Text(o, "created_at"), constants.OrderStatusCreated)
	var history struct {
		Items []platform.Result `json:"service_history"`
	}
	_ = platform.Decode(o, &history)
	for _, h := range history.Items {
		appendEvent("order_service", c.ID, platform.Text(h, "occurred_at"), platform.Text(h, "kind"))
	}
	req := platform.Request{
		ID:     c.ID,
		UserID: c.UserID,
		Role:   c.Role,
		Data:   map[string]any{"order_id": c.ID, "limit": 100},
	}
	payment := platform.Result{}
	if id := platform.Text(o, "payment_id"); id != "" {
		payment, e = clients.Payment.Get(ctx, platform.Request{ID: id, UserID: c.UserID, Role: c.Role})
		if e != nil {
			failures["payment"] = status.Code(e).String()
		} else {
			appendEvent("payment", id, platform.Text(payment, "paid_at"), platform.Text(payment, "status"))
		}
	}
	fulfillment, err := clients.Fulfillment.Get(ctx, req)
	if err != nil && status.Code(err) != codes.NotFound {
		failures["fulfillment"] = status.Code(err).String()
	}
	if err == nil {
		appendEvent("fulfillment", c.ID, platform.Text(fulfillment, "updated_at"), platform.Text(fulfillment, "status"))
	}
	logistics, err := clients.Logistics.Tracking(ctx, req)
	if err != nil && status.Code(err) != codes.NotFound {
		failures["logistics"] = status.Code(err).String()
	}
	if err == nil {
		var data struct {
			Shipments []struct {
				ID     string            `json:"id"`
				Events []platform.Result `json:"events"`
			} `json:"shipments"`
		}
		_ = platform.Decode(logistics, &data)
		for _, shipment := range data.Shipments {
			for _, event := range shipment.Events {
				appendEvent(
					"logistics",
					shipment.ID,
					platform.Text(event, "occurred_at"),
					platform.Text(event, "status"),
				)
			}
		}
	}
	cases, err := clients.Aftersale.List(ctx, req)
	if err != nil {
		failures["aftersale"] = status.Code(err).String()
	}
	tickets, err := clients.Helpdesk.List(ctx, req)
	if err != nil {
		failures["helpdesk"] = status.Code(err).String()
	}
	var casesPage, ticketsPage struct {
		Items   []platform.Result `json:"items"`
		HasMore bool              `json:"has_more"`
	}
	_ = platform.Decode(cases, &casesPage)
	_ = platform.Decode(tickets, &ticketsPage)
	for _, row := range casesPage.Items {
		if platform.Text(row, "order_id") == c.ID {
			appendEvent(
				"aftersale",
				platform.Text(row, "id"),
				platform.Text(row, "updated_at"),
				platform.Text(row, "status"),
			)
		}
	}
	for _, row := range ticketsPage.Items {
		if platform.Text(row, "order_id") == c.ID {
			appendEvent(
				"helpdesk",
				platform.Text(row, "id"),
				platform.Text(row, "created_at"),
				platform.Text(row, "status"),
			)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, platform.Text(events[i], "at"))
		b, _ := time.Parse(time.RFC3339Nano, platform.Text(events[j], "at"))
		return a.Before(b)
	})
	trimmed := len(events) > 250
	if trimmed {
		events = events[len(events)-250:]
	}
	return platform.Result{
		"order":        o,
		"payment":      payment,
		"fulfillment":  fulfillment,
		"events":       events,
		"errors":       failures,
		"partial":      trimmed || casesPage.HasMore || ticketsPage.HasMore || len(failures) > 0,
		"recent_limit": 250,
	}, nil
}
