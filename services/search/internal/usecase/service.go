package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
	"strings"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "search.usecase.Query")
	defer span.End()
	if c.Action != "query" && c.Action != "suggest" {
		return nil, status.Error(codes.Unimplemented, "unsupported search action")
	}
	cfg, e := s.repo.Get(ctx, "search-policy")
	if e != nil {
		return nil, e
	}
	_ = cfg
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
	q := strings.ToLower(platform.Text(c.Data, "query"))
	brand := platform.Text(c.Data, "brand")
	category := platform.Text(c.Data, "category")
	stockOnly, _ := c.Data["in_stock"].(bool)
	items := []platform.Result{}
	for _, p := range decoded.Items {
		if q != "" && !strings.Contains(strings.ToLower(platform.Text(p, "name")+" "+platform.Text(p, "brand")+" "+platform.Text(p, "category")), q) {
			continue
		}
		if brand != "" && platform.Text(p, "brand") != brand {
			continue
		}
		if category != "" && platform.Text(p, "category") != category {
			continue
		}
		stock, err := call(ctx, s.rpc, "inventory", child(c, "get", platform.Text(p, "id"), nil))
		if err != nil {
			return nil, err
		}
		p["available"] = platform.Int(stock, "available")
		if stockOnly && platform.Int(stock, "available") < 1 {
			continue
		}
		items = append(items, p)
	}
	sortKey := platform.Text(c.Data, "sort")
	if sortKey == "price_asc" || sortKey == "price_desc" {
		sort.SliceStable(items, func(i, j int) bool {
			a, b := platform.Int(items[i], "price_cents"), platform.Int(items[j], "price_cents")
			if sortKey == "price_desc" {
				return a > b
			}
			return a < b
		})
	}
	total := len(items)
	offset := int(platform.Int(c.Data, "offset"))
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	limit := int(platform.Int(c.Data, "limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return platform.Result{"items": items[offset:end], "total": total, "offset": offset, "limit": limit, "engine": "catalog_rpc"}, nil
}
func (s *Service) seed(ctx context.Context) error {
	r := record("search-policy", "", "policy", "ACTIVE", map[string]any{"engine": "catalog_rpc", "note": "Elasticsearch adapter is not enabled; authoritative RPC reads"})
	_, e := s.repo.InsertOnce(ctx, &r, nil)
	return e
}
func (s *Service) event(context.Context, platform.Event) error { return nil }
