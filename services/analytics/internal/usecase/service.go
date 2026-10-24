package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
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
	if c.IdempotencyKey == "" {
		return nil, bad("event idempotency_key required")
	}
	r := record(
		stable(c.UserID, c.IdempotencyKey),
		c.UserID,
		constants.AnalyticsKindBehavior,
		constants.AnalyticsStatusAccepted,
		map[string]any{
			"type":   kind,
			"sku_id": platform.Text(c.Data, "sku_id"),
			"query":  platform.Text(c.Data, "query"),
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
	r := record(
		stable(e.ID, e.UserID),
		e.UserID,
		constants.AnalyticsKindPurchaseEvent,
		constants.AnalyticsStatusAccepted,
		map[string]any{"type": e.Type, "order_id": e.AggregateID},
	)
	_, err := s.repo.InsertOnce(ctx, &r, nil)
	return err
}
