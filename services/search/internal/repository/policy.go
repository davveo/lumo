package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/platform"
)

type Policy struct {
	DefaultSort  string   `json:"default_sort"`
	MaxResults   int      `json:"max_results"`
	ExcludedIDs  []string `json:"excluded_ids"`
	HiddenBrands []string `json:"hidden_brands"`
}

func DefaultPolicy() Policy {
	return Policy{MaxResults: 100, ExcludedIDs: []string{}, HiddenBrands: []string{}}
}
func (r *Repository) Policy(ctx context.Context) (Policy, int64, error) {
	return adminconfig.Read(ctx, r.db, "search-configuration", DefaultPolicy())
}
func (r *Repository) PolicyUpsert(ctx context.Context, c platform.Request, p Policy) (platform.Result, error) {
	return adminconfig.Save(ctx, r.db, "search-configuration", p, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
}
