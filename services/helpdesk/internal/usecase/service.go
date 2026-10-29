package usecase

import (
	"context"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Close(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "helpdesk.usecase.close")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	r, e := s.repo.Mutate(
		ctx,
		domain.Record{ID: c.ID},
		false,
		func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			if e := own(c, *v); e != nil {
				return nil, nil, e
			}

			v.Status = constants.HelpdeskStatusClosed
			return nil, nil, nil

		},
	)
	if e != nil {
		return nil, e
	}
	return view(r), nil
}

func (s *Service) Create(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "helpdesk.usecase.create")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	if c.IdempotencyKey == "" {
		return nil, bad("ticket idempotency_key required")
	}
	title := platform.Text(c.Data, "title")
	body := platform.Text(c.Data, "body")
	if title == "" || body == "" || len(title) > 200 || len(body) > 10000 {
		return nil, bad("title and body required")
	}
	oid := platform.Text(c.Data, "order_id")
	if oid != "" {
		o, e := platform.NewClients(
			s.rpc,
		).Order.Get(
			ctx,
			platform.Request{ID: oid, UserID: c.UserID, Role: c.Role, IdempotencyKey: c.IdempotencyKey, Data: nil},
		)
		if e != nil {
			return nil, e
		}
		if platform.Text(o, "user_id") != c.UserID {
			return nil, status.Error(codes.PermissionDenied, "order belongs to another user")
		}
	}
	id, e := s.repo.BusinessID(ctx, "ticket", c.UserID+"\x00"+c.IdempotencyKey, stable(c.UserID, constants.HelpdeskKindTicket, c.IdempotencyKey))
	if e != nil {
		return nil, e
	}
	r := record(
		id,
		c.UserID,
		constants.HelpdeskKindTicket,
		constants.HelpdeskStatusOpen,
		map[string]any{"title": title, "body": body, "order_id": oid},
	)
	inserted, e := s.repo.InsertOnce(ctx, &r, nil)
	if e != nil {
		return nil, e
	}
	if !inserted {
		r, e = s.repo.Get(ctx, r.ID)
		if e != nil {
			return nil, e
		}
	}
	return view(r), nil
}

func (s *Service) Faq(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "helpdesk.usecase.faq")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	return platform.NewClients(s.rpc).Content.List(
		ctx,
		platform.Request{
			ID:             "",
			UserID:         c.UserID,
			Role:           c.Role,
			IdempotencyKey: c.IdempotencyKey,
			Data:           map[string]any{"kind": constants.ContentKindFaq, "limit": 100},
		},
	)
}

func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "helpdesk.usecase.get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	r, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if e = own(c, r); e != nil {
		return nil, e
	}
	out := view(r)
	msgs, e := s.repo.Children(ctx, r.ID, constants.HelpdeskKindMessage)
	if e != nil {
		return nil, e
	}
	v := []platform.Result{}
	for _, m := range msgs {
		v = append(v, view(m))
	}
	out["messages"] = v
	return out, nil
}

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "helpdesk.usecase.list")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	owner := c.UserID
	if c.Role == constants.RoleAdmin {
		owner = ""
	}
	return s.list(ctx, c, owner, constants.HelpdeskKindTicket, "")
}

func (s *Service) Reply(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "helpdesk.usecase.reply")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()

	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	r, e := s.repo.Mutate(
		ctx,
		domain.Record{ID: c.ID},
		false,
		func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			if e := own(c, *v); e != nil {
				return nil, nil, e
			}

			if v.Status == constants.HelpdeskStatusClosed {
				return nil, nil, status.Error(codes.FailedPrecondition, "ticket is closed")
			}
			body := platform.Text(c.Data, "body")
			if body == "" || len(body) > 10000 {
				return nil, nil, bad("reply body required")
			}
			if c.IdempotencyKey == "" {
				return nil, nil, bad("reply idempotency_key required")
			}
			id := stable(c.UserID, v.ID, c.IdempotencyKey)
			msg := record(
				id,
				c.UserID,
				constants.HelpdeskKindMessage,
				constants.HelpdeskStatusSent,
				map[string]any{"body": body, "role": c.Role},
			)
			msg.ParentID = v.ID
			evt := &platform.Event{
				ID:          id,
				Type:        constants.EventTicketReplied,
				AggregateID: v.ID,
				UserID:      v.UserID,
				Data:        map[string]any{"ticket_id": v.ID},
			}
			if c.Role == constants.RoleAdmin {
				v.Status = constants.HelpdeskStatusReplied
			} else {
				v.Status = constants.HelpdeskStatusOpen
			}
			return []domain.Record{msg}, evt, nil
		},
	)
	if e != nil {
		return nil, e
	}
	return view(r), nil
}

func (s *Service) seed(context.Context) error                  { return nil }
func (s *Service) event(context.Context, platform.Event) error { return nil }
