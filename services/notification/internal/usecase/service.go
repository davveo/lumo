package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "notification.usecase."+c.Action)
	defer span.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	switch c.Action {
	case "list":
		return s.list(ctx, c, c.UserID, "inbox", "")
	case "read":
		r, e := s.repo.Mutate(ctx, domain.Record{ID: c.ID}, false, func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			if e := own(c, *v); e != nil {
				return nil, nil, e
			}
			v.Status = "READ"
			return nil, nil, nil
		})
		if e != nil {
			return nil, e
		}
		return view(r), nil
	case "read_all":
		count, e := s.repo.MarkAllRead(ctx, c.UserID)
		if e != nil {
			return nil, e
		}
		return platform.Result{"marked_read": count}, nil
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported notification action")
	}
}
func (s *Service) seed(context.Context) error { return nil }
func (s *Service) event(ctx context.Context, e platform.Event) error {
	ctx, span := platform.Start(ctx, "notification.usecase.ProjectEvent")
	defer span.End()
	if e.UserID == "" {
		return nil
	}
	r := record(stable(e.ID, e.UserID, "inbox"), e.UserID, "inbox", "UNREAD", map[string]any{"event_id": e.ID, "event_type": e.Type, "business_id": e.AggregateID, "title": e.Type, "delivery_mode": "inbox_only", "external_delivery": "not_configured"})
	_, err := s.repo.InsertOnce(ctx, &r, nil)
	return err
}
