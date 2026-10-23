package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "recommendation.usecase.List")
	defer span.End()
	if c.Action != "list" {
		return nil, status.Error(codes.Unimplemented, "unsupported recommendation action")
	}
	if _, e := s.repo.Get(ctx, "recommendation-policy"); e != nil {
		return nil, e
	}
	exclude := map[string]bool{}
	orderID := platform.Text(c.Data, "order_id")
	if orderID != "" {
		if e := platform.RequireUser(c); e != nil {
			return nil, e
		}
		o, e := call(ctx, s.rpc, "order", child(c, "get", orderID, nil))
		if e != nil {
			return nil, e
		}
		if platform.Text(o, "user_id") != c.UserID && c.Role != "admin" {
			return nil, status.Error(codes.PermissionDenied, "order belongs to another user")
		}
		var snap struct {
			Items []struct {
				SKU string `json:"sku_id"`
			} `json:"items"`
		}
		if e = platform.Decode(o, &snap); e != nil {
			return nil, e
		}
		for _, it := range snap.Items {
			exclude[it.SKU] = true
		}
	}
	data, e := call(ctx, s.rpc, "catalog", child(c, "list", "", map[string]any{"limit": 100}))
	if e != nil {
		return nil, e
	}
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if e = platform.Decode(data, &decoded); e != nil {
		return nil, e
	}
	items := []platform.Result{}
	limit := int(platform.Int(c.Data, "limit"))
	if limit < 1 || limit > 20 {
		limit = 4
	}
	for _, p := range decoded.Items {
		if exclude[platform.Text(p, "id")] {
			continue
		}
		stock, err := call(ctx, s.rpc, "inventory", child(c, "get", platform.Text(p, "id"), nil))
		if err != nil {
			return nil, err
		}
		if platform.Int(stock, "available") < 1 {
			continue
		}
		p["recommendation_reason"] = "available_related_catalog"
		items = append(items, p)
		if len(items) >= limit {
			break
		}
	}
	return platform.Result{"items": items, "strategy": "catalog_available_v1", "personalized_model": false}, nil
}
func (s *Service) seed(ctx context.Context) error {
	r := record("recommendation-policy", "", "policy", "ACTIVE", map[string]any{"strategy": "catalog_available_v1", "personalized_model": false})
	_, e := s.repo.InsertOnce(ctx, &r, nil)
	return e
}
func (s *Service) event(context.Context, platform.Event) error { return nil }
