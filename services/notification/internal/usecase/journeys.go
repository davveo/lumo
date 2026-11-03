package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

// Journey delivery is an owner-checked event projection. A durable feedback
// event distinguishes queued work from a notification actually stored in inbox.
func (s *Service) projectJourney(ctx context.Context, e platform.Event) error {
	ctx, sp := platform.Start(ctx, "notification.usecase.ProjectJourney")
	defer sp.End()
	eligible, err := platform.NewClients(
		s.rpc,
	).Engagement.JourneyDeliveryCheck(
		ctx,
		platform.Request{
			ID:     e.AggregateID,
			UserID: e.UserID,
			Role:   constants.RoleSystem,
			Data:   map[string]any{"generation": platform.Int(e.Data, "generation")},
		},
	)
	if err != nil {
		return err
	}
	template, version, err := s.repo.Template(ctx, e.Type, defaultTemplate(e.Type))
	if err != nil {
		return err
	}
	deliver := eligible["eligible"] == true && template.Enabled
	feedbackType := constants.EventJourneySuppressed
	if deliver {
		feedbackType = constants.EventJourneyDelivered
	}
	feedback := platform.Event{
		ID:          platform.ID("evt"),
		Type:        feedbackType,
		UserID:      e.UserID,
		AggregateID: e.AggregateID,
		Data:        map[string]any{"generation": platform.Int(e.Data, "generation")},
	}
	id, err := s.repo.BusinessID(ctx, "notification", stable(e.ID, e.UserID, "journey"), "")
	if err != nil {
		return err
	}
	kind := constants.NotificationKindInbox
	state := constants.NotificationStatusUnread
	if !deliver {
		kind = "journey_receipt"
		state = constants.NotificationStatusRead
	}
	r := record(
		id,
		e.UserID,
		kind,
		state,
		map[string]any{
			"event_id":          e.ID,
			"event_type":        e.Type,
			"business_id":       e.AggregateID,
			"journey_id":        e.AggregateID,
			"journey_kind":      platform.Text(e.Data, "journey_kind"),
			"order_id":          platform.Text(e.Data, "order_id"),
			"sku_id":            platform.Text(e.Data, "sku_id"),
			"title":             renderNotification(template.Title, e),
			"body":              renderNotification(template.Body, e),
			"template_version":  version,
			"delivery_mode":     constants.NotificationDeliveryModeInboxOnly,
			"external_delivery": constants.NotificationExternalDeliveryNotConfigured,
		},
	)
	_, err = s.repo.InsertOnce(ctx, &r, &feedback)
	return err
}
