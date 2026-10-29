package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/content/internal/domain"
	"strings"
	"time"
)

func visibleContent(r domain.Record) bool {
	now := time.Now().UTC()
	return (r.Status == constants.ContentStatusPublished || (r.Status == constants.ContentStatusScheduled && r.PublishAt != nil && !now.Before(*r.PublishAt))) && (r.PublishAt == nil || !now.Before(*r.PublishAt)) && (r.UnpublishAt == nil || now.Before(*r.UnpublishAt))
}
func (s *Service) AdminList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "content.usecase.AdminList")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	rows, total, f, e := s.repo.AdminList(ctx, c)
	if e != nil {
		return nil, e
	}
	items := []platform.Result{}
	for _, r := range rows {
		items = append(items, view(r))
	}
	return adminquery.Result(items, total, f), nil
}
func (s *Service) AdminUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "content.usecase.AdminUpsert")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Kind, Title, Body, Status, Reason string
		Version                           int64
		PublishAt                         *time.Time `json:"publish_at"`
		UnpublishAt                       *time.Time `json:"unpublish_at"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if c.ID == "storefront-main" || c.ID == categoryNavigationID || c.ID == "" || len(c.ID) > 128 || (in.Kind != constants.ContentKindNotice && in.Kind != constants.ContentKindFaq) || strings.TrimSpace(in.Title) == "" || len([]rune(in.Title)) > 120 || strings.TrimSpace(in.Body) == "" || len(in.Body) > 20000 || in.Version < 0 || in.Version > 9007199254740990 || strings.TrimSpace(in.Reason) == "" || len([]rune(in.Reason)) > 240 {
		return nil, bad("content kind, title, body, version and change reason required")
	}
	switch in.Status {
	case constants.ContentStatusPublished, constants.ContentStatusDraft, constants.ContentStatusPaused:
	case constants.ContentStatusScheduled:
		if in.PublishAt == nil {
			return nil, bad("scheduled publication requires publish_at")
		}
	default:
		return nil, bad("invalid publication status")
	}
	if in.PublishAt != nil && in.UnpublishAt != nil && !in.PublishAt.Before(*in.UnpublishAt) {
		return nil, bad("unpublish time must follow publication")
	}
	row := record(c.ID, "", in.Kind, in.Status, map[string]any{"title": strings.TrimSpace(in.Title), "body": in.Body})
	row.Version = in.Version
	row.PublishAt = in.PublishAt
	row.UnpublishAt = in.UnpublishAt
	next, e := s.repo.AdminUpsert(ctx, row, c.UserID, in.Reason)
	return view(next), e
}
