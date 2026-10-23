package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "helpdesk.usecase."+c.Action)
	defer span.End()
	if c.Action == "faq" {
		return call(ctx, s.rpc, "content", child(c, "list", "", map[string]any{"kind": "faq", "limit": 100}))
	}
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	switch c.Action {
	case "list":
		owner := c.UserID
		if c.Role == "admin" {
			owner = ""
		}
		return s.list(ctx, c, owner, "ticket", "")
	case "get":
		r, e := s.repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if e = own(c, r); e != nil {
			return nil, e
		}
		out := view(r)
		msgs, e := s.repo.Children(ctx, r.ID, "message")
		if e != nil {
			return nil, e
		}
		v := []platform.Result{}
		for _, m := range msgs {
			v = append(v, view(m))
		}
		out["messages"] = v
		return out, nil
	case "create":
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
			o, e := call(ctx, s.rpc, "order", child(c, "get", oid, nil))
			if e != nil {
				return nil, e
			}
			if platform.Text(o, "user_id") != c.UserID {
				return nil, status.Error(codes.PermissionDenied, "order belongs to another user")
			}
		}
		r := record(stable(c.UserID, "ticket", c.IdempotencyKey), c.UserID, "ticket", "OPEN", map[string]any{"title": title, "body": body, "order_id": oid})
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
	case "reply", "close":
		r, e := s.repo.Mutate(ctx, domain.Record{ID: c.ID}, false, func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			if e := own(c, *v); e != nil {
				return nil, nil, e
			}
			if c.Action == "close" {
				v.Status = "CLOSED"
				return nil, nil, nil
			}
			if v.Status == "CLOSED" {
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
			msg := record(id, c.UserID, "message", "SENT", map[string]any{"body": body, "role": c.Role})
			msg.ParentID = v.ID
			evt := &platform.Event{ID: id, Type: "TicketReplied", AggregateID: v.ID, UserID: v.UserID, Data: map[string]any{"ticket_id": v.ID}}
			if c.Role == "admin" {
				v.Status = "REPLIED"
			} else {
				v.Status = "OPEN"
			}
			return []domain.Record{msg}, evt, nil
		})
		if e != nil {
			return nil, e
		}
		return view(r), nil
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported helpdesk action")
	}
}
func (s *Service) seed(context.Context) error                  { return nil }
func (s *Service) event(context.Context, platform.Event) error { return nil }
