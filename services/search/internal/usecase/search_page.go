package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type pagePosition struct {
	Version int    `json:"v"`
	Query   string `json:"q"`
	Catalog string `json:"c"`
	Matched int64  `json:"n"`
}

func encodePage(p pagePosition) string {
	b, _ := json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) search(ctx context.Context, c platform.Request) (platform.Result, error) {
	policy, version, e := s.repo.Policy(ctx)
	if e != nil {
		return nil, e
	}
	query, category, brand := strings.TrimSpace(
		platform.Text(c.Data, "query"),
	), strings.TrimSpace(
		platform.Text(c.Data, "category"),
	), strings.TrimSpace(
		platform.Text(c.Data, "brand"),
	)
	originalQuery := query
	exactQuery, _ := c.Data["exact_query"].(bool)
	if !exactQuery {
		query = correctQuery(query, policy.Synonyms)
	}
	attribute := strings.TrimSpace(platform.Text(c.Data, "attribute"))
	minPrice, maxPrice := platform.Int(c.Data, "min_price_cents"), platform.Int(c.Data, "max_price_cents")
	if len([]rune(attribute)) > 60 || minPrice < 0 || maxPrice < 0 || minPrice > 10000000000 ||
		maxPrice > 10000000000 ||
		(maxPrice > 0 && maxPrice < minPrice) {
		return nil, bad("invalid search filters")
	}
	stockOnly, _ := c.Data["in_stock"].(bool)
	sortKey := platform.Text(c.Data, "sort")
	if sortKey == "" {
		sortKey = policy.DefaultSort
	}
	switch sortKey {
	case "",
		constants.CatalogSortPriceAsc,
		constants.CatalogSortPriceDesc,
		constants.CatalogSortSales,
		constants.CatalogSortSalesDesc,
		constants.CatalogSortNewest:
	default:
		return nil, bad("invalid search order")
	}
	limit := int(platform.Int(c.Data, "limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := max(platform.Int(c.Data, "offset"), 0)
	if offset > 10000 {
		return nil, bad("use cursor pagination beyond offset 10000")
	}
	b, _ := json.Marshal(
		[]any{
			originalQuery,
			query,
			category,
			brand,
			sortKey,
			stockOnly,
			version,
			attribute,
			minPrice,
			maxPrice,
			exactQuery,
		},
	)
	hash := sha256.Sum256(b)
	identity := hex.EncodeToString(hash[:])
	position := pagePosition{Version: 1, Query: identity}
	if cursor := platform.Text(c.Data, "cursor"); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(decoded) > 4096 || json.Unmarshal(decoded, &position) != nil || position.Version != 1 ||
			position.Query != identity ||
			position.Matched < 0 ||
			position.Matched > 1<<50 ||
			offset != 0 {
			return nil, bad("invalid search cursor; restart this search")
		}
	}
	excluded, hidden := map[string]bool{}, map[string]bool{}
	for _, id := range policy.ExcludedIDs {
		excluded[id] = true
	}
	for _, brand := range policy.HiddenBrands {
		hidden[brand] = true
	}
	alternatives := []platform.Result{}
	items, inventory := []platform.Result{}, []platform.Result{}
	matched, skip := position.Matched, offset
	resume := position
	hasMore, exact := false, false
	clients := platform.NewClients(s.rpc)
	// Bound each request, including sparse stock filters. A continuation is
	// returned even for an empty partial page; it never masquerades as no hits.
	{
		data := map[string]any{
			"query":     query,
			"attribute": attribute, "min_price_cents": minPrice, "max_price_cents": maxPrice, "recovery": true,
			"category": category,
			"brand":    brand,
			"sort":     sortKey,
			"limit":    policy.MaxResults,
		}
		if position.Catalog != "" {
			data["cursor"] = position.Catalog
		}
		result, err := clients.Catalog.SearchProducts(ctx, platform.Request{Role: constants.RoleSystem, Data: data})
		if err != nil {
			return nil, err
		}
		var page struct {
			Items    []platform.Result `json:"items"`
			Cursors  []string          `json:"item_cursors"`
			HasMore  bool              `json:"has_more"`
			Next     string            `json:"next_cursor"`
			Recovery []platform.Result `json:"recovery_items"`
		}
		if err = platform.Decode(result, &page); err != nil {
			return nil, err
		}
		if page.HasMore && (page.Next == "" || page.Next == position.Catalog || len(page.Cursors) != len(page.Items)) {
			return nil, status.Error(codes.Internal, "catalog search did not advance")
		}
		cursorByID := map[string]string{}
		for i, row := range page.Items {
			if i < len(page.Cursors) {
				cursorByID[platform.Text(row, "id")] = page.Cursors[i]
			}
		}
		// Sorting here also preserves compatibility with older catalog peers.
		if sortKey == constants.CatalogSortPriceAsc || sortKey == constants.CatalogSortPriceDesc {
			sort.SliceStable(page.Items, func(i, j int) bool {
				a, b := platform.Int(page.Items[i], "price_cents"), platform.Int(page.Items[j], "price_cents")
				if sortKey == constants.CatalogSortPriceDesc {
					return a > b
				}
				return a < b
			})
		}
		candidates, ids := []platform.Result{}, []string{}
		for _, row := range page.Items {
			if excluded[platform.Text(row, "id")] || excluded[platform.Text(row, "product_id")] ||
				hidden[platform.Text(row, "brand")] {
				continue
			}
			if query != "" &&
				!strings.Contains(
					strings.ToLower(
						platform.Text(row, "name")+" "+platform.Text(row, "brand")+" "+platform.Text(row, "category"),
					),
					strings.ToLower(query),
				) {
				continue
			}
			if brand != "" && platform.Text(row, "brand") != brand ||
				category != "" && platform.Text(row, "category") != category {
				continue
			}
			candidates = append(candidates, row)
			ids = append(ids, platform.Text(row, "id"))
		}
		for _, row := range page.Recovery {
			ids = append(ids, platform.Text(row, "id"))
		}
		stocks := map[string]platform.Result{}
		if len(ids) > 0 {
			sort.Strings(ids)
			stock, err := clients.Inventory.BatchGet(
				ctx,
				platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"ids": ids}},
			)
			if err != nil {
				return nil, err
			}
			var rows struct {
				Items []platform.Result `json:"items"`
			}
			if err = platform.Decode(stock, &rows); err != nil {
				return nil, err
			}
			for _, row := range rows.Items {
				stocks[platform.Text(row, "sku_id")] = row
			}
		}
		for _, row := range page.Recovery {
			id := platform.Text(row, "id")
			if excluded[id] || excluded[platform.Text(row, "product_id")] || hidden[platform.Text(row, "brand")] ||
				platform.Int(stocks[id], "available") < 1 {
				continue
			}
			row["available"] = platform.Int(stocks[id], "available")
			alternatives = append(alternatives, row)
		}
		extra := false
		for _, row := range candidates {
			id := platform.Text(row, "id")
			stock, exists := stocks[id]
			if !exists {
				return nil, status.Error(codes.NotFound, "stock not found")
			}
			row["available"] = platform.Int(stock, "available")
			if stockOnly && platform.Int(stock, "available") < 1 {
				continue
			}
			matched++
			if skip > 0 {
				skip--
				continue
			}
			if len(items) < limit {
				items = append(items, row)
				inventory = append(inventory, stock)
				resume = pagePosition{Version: 1, Query: identity, Catalog: cursorByID[id], Matched: matched}
			} else {
				extra = true
			}
		}
		exact = !page.HasMore
		hasMore = extra || page.HasMore
		if !extra && page.HasMore {
			resume = pagePosition{Version: 1, Query: identity, Catalog: page.Next, Matched: matched}
		}

	}
	next := ""
	if hasMore {
		if skip > 0 {
			return nil, status.Error(codes.ResourceExhausted, "offset scan budget exceeded; use cursor pagination")
		}
		if resume.Catalog == "" && !exact {
			return nil, status.Error(codes.Internal, "catalog continuation missing")
		}
		if resume.Catalog != "" {
			next = encodePage(resume)
		}
	}
	var total any
	if exact {
		total = matched
	}
	return platform.Result{
		"items":          items,
		"original_query": originalQuery, "applied_query": query, "corrected": originalQuery != query, "alternatives": alternatives,
		"inventory_items": inventory,
		"total":           total,
		"total_exact":     exact,
		"has_more":        hasMore,
		"next_cursor":     next,
		"offset":          offset,
		"limit":           limit,
		"candidate_limit": policy.MaxResults,
		"policy_version":  version,
		"engine":          constants.SearchEngineCatalogRPC,
	}, nil
}
