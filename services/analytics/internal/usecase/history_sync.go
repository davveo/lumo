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

func (s *Service) SyncHistory(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	phase := platform.Text(c.Data, "phase")
	if phase == "" {
		phase = "payment"
	}
	if phase != "payment" && phase != "refund" {
		return nil, bad("phase must be payment or refund")
	}
	req := platform.Request{
		UserID: c.UserID,
		Role:   c.Role,
		Data:   map[string]any{"limit": 25, "cursor": platform.Text(c.Data, "cursor")},
	}
	var page platform.Result
	var e error
	clients := platform.NewClients(s.rpc)
	if phase == "payment" {
		page, e = clients.Payment.List(ctx, req)
	} else {
		page, e = clients.Refund.List(ctx, req)
	}
	if e != nil {
		return nil, e
	}
	var data struct {
		Items   []map[string]any `json:"items"`
		HasMore bool             `json:"has_more"`
		Cursor  string           `json:"next_cursor"`
	}
	if e = platform.Decode(page, &data); e != nil {
		return nil, e
	}
	orders := map[string]platform.Result{}
	if phase == "payment" {
		ids := []string{}
		for _, row := range data.Items {
			if platform.Text(row, "status") == constants.PaymentStatusSucceeded {
				ids = append(ids, platform.Text(row, "order_id"))
			}
		}
		if len(ids) > 0 {
			result, err := clients.Order.BatchGet(
				ctx,
				platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"ids": ids}},
			)
			if err != nil {
				return nil, err
			}
			orders, err = platform.IndexResults(result, "id")
			if err != nil {
				return nil, err
			}
		}
	}
	processed := 0
	for _, row := range data.Items {
		if platform.Text(row, "status") != constants.PaymentStatusSucceeded {
			continue
		}
		orderID := platform.Text(row, "order_id")
		id := "payment:" + orderID
		kind := "paid"
		atText := platform.Text(row, "paid_at")
		if phase == "refund" {
			id = "refund:" + platform.Text(row, "id")
			kind = "refund"
			atText = platform.Text(row, "updated_at")
		}
		at, e := time.Parse(time.RFC3339Nano, atText)
		if e != nil {
			return nil, status.Error(codes.FailedPrecondition, "历史交易缺少成功时间，需先核对单据")
		}
		items := any([]any{})
		if phase == "payment" {
			if order := orders[orderID]; order != nil && order["items"] != nil {
				items = order["items"]
			}
		}
		raw, _ := json.Marshal(items)
		fact := domain.TransactionFact{
			ID:          id,
			OrderID:     orderID,
			UserID:      platform.Text(row, "user_id"),
			Kind:        kind,
			AmountCents: platform.Int(row, "amount_cents"),
			Items:       string(raw),
			OccurredAt:  at.UTC(),
		}
		if phase == "payment" {
			attachAttribution(&fact, orders[orderID])
		}
		if e = s.repo.HistoryFact(ctx, fact); e != nil {
			return nil, e
		}
		processed++
	}
	next := phase
	done := false
	if data.HasMore {
		if data.Cursor == "" {
			return nil, status.Error(codes.Internal, "历史交易分页缺少游标")
		}
	} else {
		done, e = s.repo.MarkHistory(ctx, phase)
		if e != nil {
			return nil, e
		}
		data.Cursor = ""
		if phase == "payment" {
			next = "refund"
		} else {
			next = "done"
		}
	}
	return platform.Result{
		"phase":       next,
		"next_cursor": data.Cursor,
		"done":        done && phase == "refund",
		"processed":   processed,
	}, nil
}
