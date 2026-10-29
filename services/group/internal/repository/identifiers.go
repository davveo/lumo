package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (r *Repository) BusinessID(ctx context.Context, prefix, key, legacy string) (string, error) {
	return platform.ResolveBusinessID(ctx, r.DB, prefix, key, legacy, "teams")
}

func (r *Repository) createBusinessID(ctx context.Context, c platform.Request) (string, error) {
	if c.IdempotencyKey != "" {
		if len(c.IdempotencyKey) > 100 {
			return "", status.Error(codes.InvalidArgument, "idempotency key too long")
		}
		return r.BusinessID(ctx, "group", c.UserID+"\x00"+c.IdempotencyKey, "group:"+c.UserID+":"+c.IdempotencyKey)
	}
	if c.ID != "" {
		if len(c.ID) > 120 {
			return "", status.Error(codes.InvalidArgument, "group identifier too long")
		}
		return r.BusinessID(ctx, "group", "legacy-create:"+c.ID, c.ID)
	}
	return r.BusinessID(ctx, "group", "", "")
}
func (r *Repository) resolveCreateAlias(ctx context.Context, id string) (string, error) {
	alias, e := platform.LookupBusinessID(ctx, r.DB, "group", "legacy-create:"+id)
	if e != nil {
		return "", e
	}
	if alias != "" {
		return alias, nil
	}
	return id, nil
}
