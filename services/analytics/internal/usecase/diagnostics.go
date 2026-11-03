package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/attribution"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/domain"
)

func (s *Service) Track(ctx context.Context, c platform.Request) (platform.Result, error) {
	var in struct {
		attribution.Context
		Type        string `json:"type"`
		SKU         string `json:"sku_id"`
		ResultCount int64  `json:"result_count"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	in.Context = in.Context.Normalized()
	if !in.Valid() || c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 || len(in.SKU) > 150 ||
		in.ResultCount < -1 ||
		in.ResultCount > 10000000 {
		return nil, bad("invalid behavior metadata")
	}
	switch in.Type {
	case constants.AnalyticsBehaviorView,
		constants.AnalyticsBehaviorAddCart,
		constants.AnalyticsBehaviorCheckout,
		constants.AnalyticsBehaviorSearch,
		constants.AnalyticsBehaviorClick,
		constants.AnalyticsBehaviorImpression:
	default:
		return nil, bad("unsupported behavior")
	}
	if in.Type != constants.AnalyticsBehaviorSearch {
		in.ResultCount = -1
	}
	// UserID is injected by the authenticated gateway. Body claims are ignored.
	row := domain.JourneyEvent{
		ID:          stable(in.SessionKey(), c.UserID, c.IdempotencyKey),
		SessionKey:  in.SessionKey(),
		UserID:      c.UserID,
		Type:        in.Type,
		SKU:         in.SKU,
		Query:       in.Query,
		Source:      in.Source,
		ResultCount: in.ResultCount,
	}
	inserted, e := s.repo.TrackJourney(ctx, &row)
	return platform.Result{"accepted": e == nil, "duplicate": !inserted}, e
}
func (s *Service) Drilldown(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	days := int(platform.Int(c.Data, "days"))
	if days == 0 {
		days = 7
	}
	if days != 7 && days != 30 && days != 90 {
		return nil, bad("days must be 7,30,90")
	}
	dimension := platform.Text(c.Data, "dimension")
	if dimension == "" {
		dimension = "product"
	}
	if dimension != "product" && dimension != "query" && dimension != "source" && dimension != "customer" {
		return nil, bad("unsupported dimension")
	}
	keyword := attribution.CleanQuery(platform.Text(c.Data, "keyword"))
	page := int(platform.Int(c.Data, "page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		return nil, bad("page too large")
	}
	return s.repo.Drilldown(ctx, days, dimension, keyword, page)
}

func (s *Service) Tick(ctx context.Context) error { return s.repo.PruneJourneyLimits(ctx) }
