package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "engagement.usecase.list")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.EngagementKindFavorite
	}
	if kind != constants.EngagementKindFavorite && kind != constants.EngagementKindCompare &&
		kind != constants.EngagementKindHistory &&
		kind != constants.EngagementKindSearch {
		return nil, bad("kind must be favorite, compare, history or search")
	}

	return s.list(ctx, c, c.UserID, kind, "")
}
func (s *Service) Put(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "engagement.usecase.put")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.EngagementKindFavorite
	}
	if kind != constants.EngagementKindFavorite && kind != constants.EngagementKindCompare &&
		kind != constants.EngagementKindHistory &&
		kind != constants.EngagementKindSearch {
		return nil, bad("kind must be favorite, compare, history or search")
	}

	target := platform.Text(c.Data, "sku_id")
	if kind == constants.EngagementKindSearch {
		target = platform.Text(c.Data, "query")
	}
	if target == "" || len(target) > 200 {
		return nil, bad("target required")
	}
	if kind != constants.EngagementKindSearch {
		if _, e := platform.NewClients(s.rpc).Catalog.Get(ctx, platform.Request{ID: target, UserID: c.UserID, Role: c.Role, IdempotencyKey: c.IdempotencyKey, Data: nil}); e != nil {
			return nil, e
		}
	}
	if kind == constants.EngagementKindCompare {
		vs, e := s.repo.List(ctx, c.UserID, kind, "", 100, 0)
		if e != nil {
			return nil, e
		}
		if len(vs) >= 3 {
			found := false
			for _, v := range vs {
				if v.ID == stable(c.UserID, kind, target) {
					found = true
				}
			}
			if !found {
				return nil, status.Error(codes.ResourceExhausted, "compare list limited to three products")
			}
		}
	}
	r := record(
		stable(c.UserID, kind, target),
		c.UserID,
		kind,
		constants.EngagementStatusActive,
		map[string]any{"sku_id": target, "query": target},
	)
	if e := s.repo.Save(ctx, &r, nil); e != nil {
		return nil, e
	}
	return view(r), nil
}
func (s *Service) Delete(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "engagement.usecase.delete")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.EngagementKindFavorite
	}
	if kind != constants.EngagementKindFavorite && kind != constants.EngagementKindCompare &&
		kind != constants.EngagementKindHistory &&
		kind != constants.EngagementKindSearch {
		return nil, bad("kind must be favorite, compare, history or search")
	}

	id := c.ID
	if target := platform.Text(c.Data, "sku_id"); target != "" {
		id = stable(c.UserID, kind, target)
	}
	if e := s.repo.Delete(ctx, id, c.UserID); e != nil {
		return nil, e
	}
	return platform.Result{"deleted": true}, nil
}
func (s *Service) Clear(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "engagement.usecase.clear")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.EngagementKindFavorite
	}
	if kind != constants.EngagementKindFavorite && kind != constants.EngagementKindCompare &&
		kind != constants.EngagementKindHistory &&
		kind != constants.EngagementKindSearch {
		return nil, bad("kind must be favorite, compare, history or search")
	}

	if e := s.repo.DeleteKind(ctx, c.UserID, kind); e != nil {
		return nil, e
	}
	return platform.Result{"cleared": true}, nil
}

func (s *Service) seed(context.Context) error                  { return nil }
func (s *Service) event(context.Context, platform.Event) error { return nil }
