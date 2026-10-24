package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "analytics.usecase."+c.Action)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	switch c.Action {
	case "record":
		kind := platform.Text(c.Data, "type")
		if kind != "view" && kind != "click" && kind != "impression" && kind != "search" {
			return nil, bad("unsupported behavior type")
		}
		if c.IdempotencyKey == "" {
			return nil, bad("event idempotency_key required")
		}
		r := record(stable(c.UserID, c.IdempotencyKey), c.UserID, "behavior", "ACCEPTED", map[string]any{"type": kind, "sku_id": platform.Text(c.Data, "sku_id"), "query": platform.Text(c.Data, "query")})
		evt := platform.Event{ID: r.ID, Type: "BehaviorAccepted", AggregateID: r.ID, UserID: c.UserID, Data: view(r)}
		inserted, e := s.repo.InsertOnce(ctx, &r, &evt)
		if e != nil {
			return nil, e
		}
		return platform.Result{"event_id": r.ID, "accepted": true, "duplicate": !inserted}, nil
	case "list":
		if e := platform.RequireAdmin(c); e != nil {
			return nil, e
		}
		return s.list(ctx, c, "", "behavior", "")
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported analytics action")
	}
}
func (s *Service) seed(context.Context) error { return nil }
func (s *Service) event(ctx context.Context, e platform.Event) error {
	if e.UserID == "" {
		return nil
	}
	r := record(stable(e.ID, e.UserID), e.UserID, "purchase_event", "ACCEPTED", map[string]any{"type": e.Type, "order_id": e.AggregateID})
	_, err := s.repo.InsertOnce(ctx, &r, nil)
	return err
}
