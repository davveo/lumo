package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/group/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"time"
)

func (s *Service) AdminList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "group.usecase.AdminList")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.AdminList(ctx, c)
}
func (s *Service) Rules(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "group.usecase.Rules")
	defer span.End()
	return s.Repo.RulesAdmin(ctx, c, true)
}
func (s *Service) RulesAdmin(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "group.usecase.RulesAdmin")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.RulesAdmin(ctx, c, false)
}
func (s *Service) RuleUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "group.usecase.RuleUpsert")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var row domain.Rule
	if e := platform.Decode(c.Data, &row); e != nil {
		return nil, e
	}
	row.ID = c.ID
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	if row.ID == "" || len(row.ID) > 120 || len(row.SKUID) > 80 || row.ActivityID == "" || len(row.ActivityID) > 120 || strings.TrimSpace(row.Name) == "" || len([]rune(row.Name)) > 120 || row.Required < 2 || row.Required > 10 || row.DurationMinutes < 1 || row.DurationMinutes > 10080 || row.Version < 0 || row.Version > 9007199254740990 || reason == "" || len([]rune(reason)) > 240 || (row.StartsAt != nil && row.EndsAt != nil && !row.StartsAt.Before(*row.EndsAt)) {
		return nil, status.Error(codes.InvalidArgument, "valid group rule, size, duration, version and reason required")
	}
	if row.SKUID != "" {
		if _, e := platform.NewClients(s.RPC).Catalog.Get(ctx, platform.Request{ID: row.SKUID, UserID: c.UserID, Role: c.Role}); e != nil {
			return nil, e
		}
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
		if a.ID == row.ActivityID && a.Kind == constants.CampaignKindGroup {
			found = true
		}
	}
	if !found {
		return nil, status.Error(codes.FailedPrecondition, "create a group quota rule before linking the group policy")
	}
	return s.Repo.RuleUpsert(ctx, row, c.UserID, reason)
}
func (s *Service) applyRule(ctx context.Context, c platform.Request) (platform.Request, error) {
	ctx, span := platform.Start(ctx, "group.usecase.applyRule")
	defer span.End()
	id := platform.Text(c.Data, "rule_id")
	if id == "" {
		id = "group-default"
	}
	rule, e := s.Repo.Rule(ctx, id)
	if e != nil {
		return c, e
	}
	now := time.Now().UTC()
	if !rule.Active || (rule.StartsAt != nil && now.Before(*rule.StartsAt)) || (rule.EndsAt != nil && !now.Before(*rule.EndsAt)) || (rule.SKUID != "" && rule.SKUID != platform.Text(c.Data, "sku_id")) {
		return c, status.Error(codes.FailedPrecondition, "group rule is inactive or SKU does not match")
	}
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	c.Data["required"] = rule.Required
	c.Data["duration_minutes"] = rule.DurationMinutes
	c.Data["rule_id"] = rule.ID
	c.Data["rule_version"] = rule.Version
	c.Data["activity_id"] = rule.ActivityID
	return c, nil
}
