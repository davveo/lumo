package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
	"strings"
)

func (s *Service) Query(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "search.usecase.query")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	return s.search(ctx, c)
}
func (s *Service) Suggest(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "search.usecase.suggest")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	return s.search(ctx, c)
}
func (s *Service) search(ctx context.Context, c platform.Request) (platform.Result, error) {
	policy, policyVersion, e := s.repo.Policy(ctx)
	if e != nil {
		return nil, e
	}
	excluded := map[string]bool{}
	for _, id := range policy.ExcludedIDs {
		excluded[id] = true
	}
	hidden := map[string]bool{}
	for _, brand := range policy.HiddenBrands {
		hidden[brand] = true
	}
	// Apply constraints to the full authoritative catalog before taking a
	// bounded candidate window. Only this server constructs the internal role.
	sortKey := platform.Text(c.Data, "sort")
	catalogSort := sortKey
	if catalogSort == "" {
		catalogSort = policy.DefaultSort
	}
	switch catalogSort {
	case constants.CatalogSortDefault,
		constants.CatalogSortPriceAsc,
		constants.CatalogSortPriceDesc,
		constants.CatalogSortSales,
		constants.CatalogSortSalesDesc,
		constants.CatalogSortNewest:
	default:
		catalogSort = ""
	}
	catalogRequest := platform.Request{
		Role: constants.RoleSystem,
		Data: map[string]any{
			"query": strings.TrimSpace(
				platform.Text(c.Data, "query"),
			), "category": strings.TrimSpace(platform.Text(c.Data, "category")),
			"brand": strings.TrimSpace(platform.Text(c.Data, "brand")), "sort": catalogSort, "limit": policy.MaxResults,
		},
	}
	data, e := platform.NewClients(s.rpc).Catalog.SearchProducts(ctx, catalogRequest)
	if e != nil {
		return nil, e
	}
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if e = platform.Decode(data, &decoded); e != nil {
		return nil, e
	}
	q := strings.ToLower(strings.TrimSpace(platform.Text(c.Data, "query")))
	brand := strings.TrimSpace(platform.Text(c.Data, "brand"))
	category := strings.TrimSpace(platform.Text(c.Data, "category"))
	stockOnly, _ := c.Data["in_stock"].(bool)
	candidates := []map[string]any{}
	ids, seen := []string{}, map[string]bool{}
	for _, p := range decoded.Items {
		if excluded[platform.Text(p, "id")] || excluded[platform.Text(p, "product_id")] || hidden[platform.Text(p, "brand")] {
			continue
		}
		if q != "" &&
			!strings.Contains(
				strings.ToLower(
					platform.Text(p, "name")+" "+platform.Text(p, "brand")+" "+platform.Text(p, "category"),
				),
				q,
			) {
			continue
		}
		if brand != "" && platform.Text(p, "brand") != brand {
			continue
		}
		if category != "" && platform.Text(p, "category") != category {
			continue
		}
		candidates = append(candidates, p)
		id := platform.Text(p, "id")
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	stocks := map[string]map[string]any{}
	if len(ids) > 0 {
		sort.Strings(ids)
		batch, err := platform.NewClients(
			s.rpc,
		).Inventory.BatchGet(
			ctx,
			platform.Request{
				ID:             "",
				UserID:         c.UserID,
				Role:           c.Role,
				IdempotencyKey: c.IdempotencyKey,
				Data:           map[string]any{"ids": ids},
			},
		)
		if err != nil {
			return nil, err
		}
		var rows struct {
			Items []map[string]any `json:"items"`
		}
		if err = platform.Decode(batch, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows.Items {
			stocks[platform.Text(row, "sku_id")] = row
		}
	}
	items := []platform.Result{}
	for _, p := range candidates {
		stock, exists := stocks[platform.Text(p, "id")]
		if !exists {
			return nil, status.Error(codes.NotFound, "stock not found")
		}
		p["available"] = platform.Int(stock, "available")
		if stockOnly && platform.Int(stock, "available") < 1 {
			continue
		}
		items = append(items, p)
	}
	if sortKey == constants.CatalogSortPriceAsc || sortKey == constants.CatalogSortPriceDesc {
		sort.SliceStable(items, func(i, j int) bool {
			a, b := platform.Int(items[i], "price_cents"), platform.Int(items[j], "price_cents")
			if sortKey == constants.CatalogSortPriceDesc {
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
	selected := items[offset:end]
	inventory := []map[string]any{}
	for _, product := range selected {
		inventory = append(inventory, stocks[platform.Text(product, "id")])
	}
	return platform.Result{
		"items":           selected,
		"inventory_items": inventory,
		"total":           total,
		"offset":          offset,
		"limit":           limit,
		"candidate_limit": policy.MaxResults,
		"policy_version":  policyVersion,
		"engine":          constants.SearchEngineCatalogRPC,
	}, nil
}
func (s *Service) seed(ctx context.Context) error {
	r := record(
		"search-policy",
		"",
		constants.SearchKindPolicy,
		constants.SearchStatusActive,
		map[string]any{
			"engine": constants.SearchEngineCatalogRPC,
			"note":   "Elasticsearch adapter is not enabled; authoritative RPC reads",
		},
	)
	_, e := s.repo.InsertOnce(ctx, &r, nil)
	return e
}
func (s *Service) event(context.Context, platform.Event) error { return nil }
