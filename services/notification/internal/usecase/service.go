package usecase

import (
	"context"
	"time"

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

	if _, paged := c.Data["page"]; !paged {
		return s.list(ctx, c, c.UserID, constants.NotificationKindInbox, "")
	}
	page, size := int(platform.Int(c.Data, "page")), int(platform.Int(c.Data, "page_size"))
	if page < 1 || page > 100000 {
		return nil, bad("page must be 1..100000")
	}
	if size == 0 {
		size = 10
	}
	if size < 1 || size > 50 {
		return nil, bad("page_size must be 1..50")
	}
	filter := platform.Text(c.Data, "filter")
	if filter == "" {
		filter = constants.MessageFilterAll
	}
	switch filter {
	case constants.MessageFilterAll,
		constants.MessageFilterUnread,
		constants.MessageTypeOrder,
		constants.MessageTypeActivity,
		constants.MessageTypeService:
	default:
		return nil, bad("unsupported inbox filter")
	}
	result, e := s.repo.InboxPage(ctx, c.UserID, filter, page, size)
	if e != nil {
		return nil, e
	}
	items := []platform.Result{}
	for _, row := range result.Items {
		items = append(items, view(row))
	}
	return platform.Result{
		"items":        items,
		"total":        result.Total,
		"all_total":    result.AllTotal,
		"unread_total": result.UnreadTotal,
		"page":         result.Page,
		"page_size":    result.PageSize,
		"filter":       filter,
	}, nil
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
	if e.Type == constants.EventCouponExpiring {
		settings, err := platform.NewClients(
			s.rpc,
		).Coupon.ReminderGet(
			ctx,
			platform.Request{UserID: e.UserID, Role: constants.RoleSystem},
		)
		if err != nil {
			return err
		}
		if settings["enabled"] != true || platform.Int(settings, "expiring_count") < 1 {
			return nil
		}
		data := map[string]any{}
		for k, v := range e.Data {
			data[k] = v
		}
		data["coupon_count"] = settings["expiring_count"]
		if at, err := time.Parse(time.RFC3339Nano, platform.Text(settings, "next_expiry")); err == nil {
			data["expires_at"] = at.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04")
		}
		e.Data = data
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
