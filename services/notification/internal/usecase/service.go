package usecase

import (
	"context"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification/internal/domain"
)

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "notification.usecase.list")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	return s.list(ctx, c, c.UserID, constants.NotificationKindInbox, "")
}

func (s *Service) Read(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "notification.usecase.read")
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
			v.Status = constants.NotificationStatusRead
			return nil, nil, nil
		},
	)
	if e != nil {
		return nil, e
	}
	return view(r), nil
}

func (s *Service) ReadAll(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "notification.usecase.read_all")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	count, e := s.repo.MarkAllRead(ctx, c.UserID)
	if e != nil {
		return nil, e
	}
	return platform.Result{"marked_read": count}, nil
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
	template, version, err := s.repo.Template(ctx, e.Type, defaultTemplate(e.Type))
	if err != nil {
		return err
	}
	if !template.Enabled {
		return nil
	}
	key := stable(e.ID, e.UserID, constants.NotificationKindInbox)
	id, idErr := s.repo.BusinessID(ctx, "notification", key, key)
	if idErr != nil {
		return idErr
	}
	r := record(
		id,
		e.UserID,
		constants.NotificationKindInbox,
		constants.NotificationStatusUnread,
		map[string]any{
			"event_id":    e.ID,
			"event_type":  e.Type,
			"sku_id":      platform.Text(e.Data, "sku_id"),
			"business_id": e.AggregateID,
			"title":       renderNotification(template.Title, e),
			"body":        renderNotification(template.Body, e), "template_version": version,
			"delivery_mode":     constants.NotificationDeliveryModeInboxOnly,
			"external_delivery": constants.NotificationExternalDeliveryNotConfigured,
		},
	)
	_, err = s.repo.InsertOnce(ctx, &r, nil)
	return err
}
