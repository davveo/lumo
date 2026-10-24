package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "notification.usecase."+c.Action)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	switch c.Action {
	case constants.ActionList:
		return s.list(ctx, c, c.UserID, constants.NotificationKindInbox, "")
	case constants.ActionRead:
		r, e := s.repo.Mutate(
			ctx,
			domain.Record{ID: c.ID},
			false,
			func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
				if e := own(c, *v); e != nil {
					return nil, nil, e
				}
				v.Status = constants.NotificationStatusRead
				return nil, nil, nil
			},
		)
		if e != nil {
			return nil, e
		}
		return view(r), nil
	case constants.ActionReadAll:
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
func (s *Service) event(ctx context.Context, e platform.Event) (traceErr error) {
	ctx, span := platform.Start(ctx, "notification.usecase.ProjectEvent")
	defer span.End()
	platform.TraceRequest(ctx, e)
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
	if e.UserID == "" {
		return nil
	}
	r := record(
		stable(e.ID, e.UserID, constants.NotificationKindInbox),
		e.UserID,
		constants.NotificationKindInbox,
		constants.NotificationStatusUnread,
		map[string]any{
			"event_id":          e.ID,
			"event_type":        e.Type,
			"business_id":       e.AggregateID,
			"title":             e.Type,
			"delivery_mode":     constants.NotificationDeliveryModeInboxOnly,
			"external_delivery": constants.NotificationExternalDeliveryNotConfigured,
		},
	)
	_, err := s.repo.InsertOnce(ctx, &r, nil)
	return err
}
