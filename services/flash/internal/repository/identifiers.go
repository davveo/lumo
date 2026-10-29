package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
)

func (r *Repository) BusinessID(ctx context.Context, prefix, key, legacy string) (string, error) {
	return platform.ResolveBusinessID(ctx, r.DB, prefix, key, legacy, "requests")
}
