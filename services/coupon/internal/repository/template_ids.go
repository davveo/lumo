package repository

import (
	"context"
	"strings"
	"unicode"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TemplatesByIDs is an internal bounded lookup used by marketing decisions.
// It shares the public template status projection and reads only requested IDs.
func (r *Repository) TemplatesByIDs(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "repository.coupon.TemplatesByIDs")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	ids := []string{}
	switch input := c.Data["ids"].(type) {
	case []string:
		ids = input
	case []any:
		for _, value := range input {
			id, ok := value.(string)
			if !ok {
				return nil, status.Error(codes.InvalidArgument, "template ids must be strings")
			}
			ids = append(ids, id)
		}
	default:
		return nil, status.Error(codes.InvalidArgument, "template ids required")
	}
	if len(ids) < 1 || len(ids) > 50 {
		return nil, status.Error(codes.InvalidArgument, "1..50 template ids required")
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 120 || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
			return nil, status.Error(codes.InvalidArgument, "invalid template id")
		}
		if !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	templates := []domain.Template{}
	if err := r.DB.WithContext(ctx).Where("id IN ?", unique).Order("id").Find(&templates).Error; err != nil {
		return nil, err
	}
	items, now, err := r.templateItems(ctx, c, templates)
	if err != nil {
		return nil, err
	}
	return platform.Result{"items": items, "total": len(items), "server_time": now}, nil
}
