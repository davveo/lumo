package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/platform"
)

type Policy struct {
	FavoriteWeight   float64  `json:"favorite_weight"`
	CompareWeight    float64  `json:"compare_weight"`
	HistoryWeight    float64  `json:"history_weight"`
	PurchaseWeight   float64  `json:"purchase_weight"`
	SearchWeight     float64  `json:"search_weight"`
	PopularityWeight float64  `json:"popularity_weight"`
	FreshnessWeight  float64  `json:"freshness_weight"`
	PersonalWeight   float64  `json:"personal_weight"`
	SimilarWeight    float64  `json:"similar_weight"`
	PinnedIDs        []string `json:"pinned_ids"`
	ExcludedIDs      []string `json:"excluded_ids"`
}

func DefaultPolicy() Policy {
	return Policy{FavoriteWeight: 8, CompareWeight: 5, HistoryWeight: 3, PurchaseWeight: 12, SearchWeight: 4, PopularityWeight: .18, FreshnessWeight: .35, PersonalWeight: 6, SimilarWeight: 9, PinnedIDs: []string{}, ExcludedIDs: []string{}}
}
func (r *Repository) Policy(ctx context.Context) (Policy, int64, error) {
	return adminconfig.Read(ctx, r.db, "recommendation-policy", DefaultPolicy())
}
func (r *Repository) PolicyUpsert(ctx context.Context, c platform.Request, p Policy) (platform.Result, error) {
	return adminconfig.Save(ctx, r.db, "recommendation-policy", p, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
}
