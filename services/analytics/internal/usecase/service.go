package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"unicode/utf8"
)

func (s *Service) Record(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "analytics.usecase.Record")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	kind := platform.Text(c.Data, "type")
	if kind != constants.AnalyticsBehaviorView && kind != constants.AnalyticsBehaviorClick &&
		kind != constants.AnalyticsBehaviorImpression &&
		kind != constants.AnalyticsBehaviorSearch {
		return nil, bad("unsupported behavior type")
	}
	if utf8.RuneCountInString(platform.Text(c.Data, "query")) > 200 || len(platform.Text(c.Data, "sku_id")) > 150 || len(platform.Text(c.Data, "page")) > 240 || len(platform.Text(c.Data, "source")) > 40 {
		return nil, bad("behavior metadata too long")
	}
	if c.IdempotencyKey == "" {
		return nil, bad("event idempotency_key required")
	}
	productName := ""
	if sku := platform.Text(c.Data, "sku_id"); sku != "" && s.rpc != nil {
		if product, e := platform.NewClients(s.rpc).Catalog.Get(ctx, platform.Request{ID: sku, UserID: c.UserID, Role: c.Role}); e == nil {
			productName = platform.Text(product, "name")
		}
	}
	r := record(
		stable(c.UserID, c.IdempotencyKey),
		c.UserID,
		constants.AnalyticsKindBehavior,
		constants.AnalyticsStatusAccepted,
		map[string]any{
			"type": kind, "product_name": productName,
			"sku_id": platform.Text(c.Data, "sku_id"),
			"query":  platform.Text(c.Data, "query"),
			"page":   platform.Text(c.Data, "page"), "source": platform.Text(c.Data, "source"),
		},
	)
	evt := platform.Event{
		ID:          r.ID,
		Type:        constants.EventBehaviorAccepted,
		AggregateID: r.ID,
		UserID:      c.UserID,
		Data:        view(r),
	}
	inserted, e := s.repo.InsertOnce(ctx, &r, &evt)
	if e != nil {
		return nil, e
	}
	return platform.Result{"event_id": r.ID, "accepted": true, "duplicate": !inserted}, nil
}
func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "analytics.usecase.List")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.list(ctx, c, "", constants.AnalyticsKindBehavior, "")
}

func (s *Service) seed(context.Context) error { return nil }
func (s *Service) event(ctx context.Context, e platform.Event) error {
	if e.UserID == "" {
		return nil
	}
	data := map[string]any{"type": e.Type, "business_id": e.AggregateID}
	if e.Type == constants.EventRefundSucceeded {
		data["refund_id"] = e.AggregateID
		data["order_id"] = platform.Text(e.Data, "order_id")
		data["amount_cents"] = platform.Int(e.Data, "amount_cents")
	} else {
		data["order_id"] = e.AggregateID
	}
	r := record(
		stable(e.ID, e.UserID),
		e.UserID,
		constants.AnalyticsKindPurchaseEvent,
		constants.AnalyticsStatusAccepted,
		data,
	)
	_, err := s.repo.InsertOnce(ctx, &r, nil)
	return err
}
