package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/flash/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"time"
)

func openSession(row domain.Session) bool {
	now := time.Now().UTC()
	return row.Active && (row.StartsAt == nil || !now.Before(*row.StartsAt)) && (row.EndsAt == nil || now.Before(*row.EndsAt))
}
func (s *Service) AdminList(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "flash.usecase.AdminList")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.AdminList(ctx, c)
}
func (s *Service) RequestsAdmin(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "flash.usecase.RequestsAdmin")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.RequestsAdmin(ctx, c)
}
func (s *Service) SessionGet(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "flash.usecase.SessionGet")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var row domain.Session
	var e error
	if c.ID != "" {
		row, e = s.Repo.Session(ctx, c.ID)
	} else {
		row, e = s.Repo.SessionForActivity(ctx, platform.Text(c.Data, "activity_id"))
	}
	if e != nil {
		return nil, e
	}
	out := platform.Map(row)
	out["open"] = openSession(row)
	return out, nil
}
func (s *Service) Upsert(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "flash.usecase.Upsert")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var row domain.Session
	if e := platform.Decode(c.Data, &row); e != nil {
		return nil, e
	}
	row.ID = c.ID
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	if row.ID == "" || len(row.ID) > 120 || row.SKUID == "" || len(row.SKUID) > 80 || row.ActivityID == "" || len(row.ActivityID) > 120 || strings.TrimSpace(row.Name) == "" || len([]rune(row.Name)) > 120 || row.Version < 0 || row.Version > 9007199254740990 || reason == "" || len([]rune(reason)) > 240 || (row.StartsAt != nil && row.EndsAt != nil && !row.StartsAt.Before(*row.EndsAt)) {
		return nil, status.Error(codes.InvalidArgument, "valid session, time window, version and reason required")
	}
	if _, e := platform.NewClients(s.RPC).Catalog.Get(ctx, platform.Request{ID: row.SKUID, UserID: c.UserID, Role: c.Role}); e != nil {
		return nil, e
	}
	activities, e := platform.NewClients(s.RPC).Campaign.AdminList(ctx, platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"keyword": row.ActivityID, "page_size": 100}})
	if e != nil {
		return nil, e
	}
	var list struct{ Items []struct{ ID, Kind string } }
	if e = platform.Decode(activities, &list); e != nil {
		return nil, e
	}
	found := false
	for _, a := range list.Items {
		if a.ID == row.ActivityID && a.Kind == constants.CampaignKindFlash {
			found = true
		}
	}
	if !found {
		return nil, status.Error(codes.FailedPrecondition, "create a flash quota rule before linking the session")
	}
	return s.Repo.Upsert(ctx, row, c.UserID, reason)
}
