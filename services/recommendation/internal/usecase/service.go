package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const strategy = constants.RecommendationStrategyHybridContentV2

type recommendationRequest struct {
	Scene     string `json:"scene"`
	ProductID string `json:"product_id"`
	OrderID   string `json:"order_id"`
	Limit     int    `json:"limit"`
}
type signalRows struct {
	Favorite []map[string]any `json:"favorite"`
	History  []map[string]any `json:"history"`
	Compare  []map[string]any `json:"compare"`
	Search   []map[string]any `json:"search"`
	Orders   []map[string]any `json:"orders"`
}
type bootstrapInput struct {
	Catalog   []map[string]any `json:"catalog_items"`
	Inventory []map[string]any `json:"inventory_items"`
	Signals   signalRows       `json:"signals"`
	Errors    []string         `json:"signal_errors"`
}

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "recommendation.usecase.list")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	in, err := recommendationInput(ctx, c)
	if err != nil {
		return nil, err
	}
	if _, err = s.repo.Get(ctx, "recommendation-policy"); err != nil {
		return nil, err
	}
	// Public requests always read server-owned signals and catalog data.
	base, err := s.peerRows(
		ctx,
		platform.NewClients(s.rpc).Catalog.List,
		platform.Request{Role: constants.RoleSystem, Data: map[string]any{"limit": 100}},
	)
	if err != nil {
		return nil, err
	}
	var signals signalRows
	degraded := false
	if c.UserID != "" {
		signals, degraded = s.readSignals(ctx, c)
	}
	result, _, err := s.rank(ctx, c, in, base, nil, signals, degraded)
	return result, err
}
func (s *Service) RankBootstrap(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "recommendation.usecase.rank_bootstrap")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	if c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "bootstrap ranking is internal")
	}
	in, err := recommendationInput(ctx, c)
	if err != nil {
		return nil, err
	}
	if in.Scene != constants.RecommendationSceneHome || in.OrderID != "" || in.ProductID != "" {
		return nil, bad("bootstrap ranking only accepts home scene")
	}
	if _, err = s.repo.Get(ctx, "recommendation-policy"); err != nil {
		return nil, err
	}
	var input bootstrapInput
	if err = platform.Decode(c.Data, &input); err != nil || len(input.Catalog) > 500 || len(input.Inventory) > 1000 {
		return nil, bad("invalid bootstrap recommendation input")
	}
	result, snapshot, err := s.rank(ctx, c, in, input.Catalog, input.Inventory, input.Signals, len(input.Errors) > 0)
	if err != nil {
		return nil, err
	}
	result["stock_snapshot"] = snapshot
	return result, nil
}
func recommendationInput(ctx context.Context, c platform.Request) (recommendationRequest, error) {
	if ctx.Err() != nil {
		return recommendationRequest{}, status.FromContextError(ctx.Err()).Err()
	}
	var in recommendationRequest
	if err := platform.Decode(c.Data, &in); err != nil {
		return recommendationRequest{}, bad("invalid recommendation request")
	}
	if in.Scene == "" {
		in.Scene = constants.RecommendationSceneHome
		if in.OrderID != "" {
			in.Scene = constants.RecommendationScenePostPurchase
		}
	}
	if in.Scene != constants.RecommendationSceneHome && in.Scene != constants.RecommendationSceneSimilar &&
		in.Scene != constants.RecommendationScenePostPurchase {
		return recommendationRequest{}, bad("scene must be home, similar or post_purchase")
	}
	if in.Limit == 0 {
		in.Limit = 4
	}
	if in.Limit < 1 || in.Limit > 20 {
		return recommendationRequest{}, bad("limit must be between 1 and 20")
	}
	if !validID(in.ProductID) || !validID(in.OrderID) {
		return recommendationRequest{}, bad("invalid product or order ID")
	}
	if in.Scene == constants.RecommendationSceneSimilar && in.ProductID == "" {
		return recommendationRequest{}, bad("similar recommendations require product_id")
	}
	if in.Scene == constants.RecommendationScenePostPurchase && in.OrderID == "" {
		return recommendationRequest{}, bad("post_purchase recommendations require order_id")
	}
	return in, nil
}

func (s *Service) rank(
	ctx context.Context,
	c platform.Request,
	in recommendationRequest,
	base, stockRows []map[string]any,
	signals signalRows,
	degraded bool,
) (platform.Result, []map[string]any, error) {
	if c.UserID == "" {
		signals = signalRows{}
	}
	if ctx.Err() != nil {
		return nil, nil, status.FromContextError(ctx.Err()).Err()
	}
	signals, foreign := ownedSignals(signals, c.UserID)
	degraded = degraded || foreign
	contextOrder := map[string]any{}
	if in.OrderID != "" {
		if err := platform.RequireUser(c); err != nil {
			return nil, nil, err
		}
		result, err := platform.NewClients(
			s.rpc,
		).Order.Get(
			ctx,
			platform.Request{ID: in.OrderID, UserID: c.UserID, Role: constants.RoleUser},
		)
		if err != nil {
			return nil, nil, err
		}
		if platform.Text(result, "user_id") != c.UserID {
			return nil, nil, status.Error(codes.PermissionDenied, "order belongs to another user")
		}
		contextOrder = map[string]any(result)
	}
	metadata := map[string]product{}
	for _, row := range base {
		p := productFrom(row)
		if p.id != "" {
			metadata[p.id] = p
			metadata[p.rawID] = p
		}
	}
	seeds := seedIDs(signals, contextOrder, in.ProductID)
	missing := []string{}
	for _, id := range seeds {
		if _, ok := metadata[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		rows, err := s.peerRows(
			ctx,
			platform.NewClients(s.rpc).Catalog.BatchGet,
			platform.Request{Role: constants.RoleSystem, Data: map[string]any{"ids": missing}},
		)
		if err != nil {
			if in.ProductID != "" {
				return nil, nil, err
			}
			degraded = true
		} else {
			for _, row := range rows {
				p := productFrom(row)
				metadata[p.rawID] = p
				if _, ok := metadata[p.id]; !ok {
					metadata[p.id] = p
				}
			}
		}
	}
	// Resolve option/legacy SKU metadata to the actual parent in one bounded read.
	parents, seen := []string{}, map[string]bool{}
	for _, id := range seeds {
		if p, ok := metadata[id]; ok && p.id != p.rawID {
			if parent, exists := metadata[p.id]; (!exists || parent.rawID != p.id) && !seen[p.id] {
				parents = append(parents, p.id)
				seen[p.id] = true
			}
		}
	}
	if len(parents) > 0 {
		rows, err := s.peerRows(
			ctx,
			platform.NewClients(s.rpc).Catalog.BatchGet,
			platform.Request{Role: constants.RoleSystem, Data: map[string]any{"ids": parents}},
		)
		if err != nil {
			degraded = true
		} else {
			for _, row := range rows {
				p := productFrom(row)
				metadata[p.id] = p
				metadata[p.rawID] = p
			}
		}
	}
	var contextProduct *product
	if in.ProductID != "" {
		p, ok := metadata[in.ProductID]
		if parent, exists := metadata[p.id]; exists {
			p = parent
		}
		if !ok || !p.active {
			return nil, nil, status.Error(codes.NotFound, "recommendation product unavailable")
		}
		contextProduct = &p
	}
	profile := buildProfile(signals, metadata, time.Now())
	exclude := map[string]bool{}
	for id := range profile.purchased {
		exclude[id] = true
	}
	for _, row := range rowsOf(contextOrder["items"]) {
		id := platform.Text(row, "sku_id")
		if id == "" {
			continue
		}
		exclude[id] = true
		if p, ok := metadata[id]; ok {
			exclude[p.id] = true
		}
	}
	if contextProduct != nil {
		exclude[contextProduct.id] = true
		exclude[contextProduct.rawID] = true
	}
	categories, brands, queries := profile.recall(contextProduct, contextOrder, metadata)
	recalled := []map[string]any{}
	if len(categories)+len(brands)+len(queries) > 0 {
		recall, err := platform.NewClients(s.rpc).Catalog.RecommendationCandidates(ctx, platform.Request{
			Role: constants.RoleSystem,
			Data: map[string]any{"categories": categories, "brands": brands, "queries": queries, "limit": 200},
		},
		)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, status.FromContextError(ctx.Err()).Err()
			}
			degraded = true
		} else {
			var decoded struct {
				Items    []map[string]any `json:"items"`
				Degraded bool             `json:"degraded"`
			}
			if err = platform.Decode(recall, &decoded); err != nil {
				degraded = true
			} else {
				recalled = decoded.Items
				degraded = degraded || decoded.Degraded
			}
		}
	}
	// Targeted recall leads the bounded pool; the catalog supplies exploration.
	candidates, unique := []product{}, map[string]bool{}
	for _, source := range [][]map[string]any{recalled, base} {
		for _, row := range source {
			p := productFrom(row)
			if p.id == "" || !p.active || unique[p.id] || excludedProduct(p, exclude) {
				continue
			}
			unique[p.id] = true
			candidates = append(candidates, p)
			if len(candidates) >= 300 {
				break
			}
		}
		if len(candidates) >= 300 {
			break
		}
	}
	stocks := map[string]map[string]any{}
	for _, row := range stockRows {
		if id := platform.Text(row, "sku_id"); id != "" {
			stocks[id] = row
		}
	}
	stockIDs := []string{}
	for _, p := range candidates {
		if _, ok := stocks[p.id]; !ok {
			stockIDs = append(stockIDs, p.id)
		}
	}
	if len(stockIDs) > 0 {
		rows, err := s.peerRows(
			ctx,
			platform.NewClients(s.rpc).Inventory.BatchGet,
			platform.Request{Data: map[string]any{"ids": stockIDs}},
		)
		if err != nil {
			if len(stockRows) == 0 || len(stocks) == 0 {
				return nil, nil, err
			}
			degraded = true
		} else {
			for _, row := range rows {
				stocks[platform.Text(row, "sku_id")] = row
			}
		}
	}
	sellable := []product{}
	for _, p := range candidates {
		stock, ok := stocks[p.id]
		if !ok {
			degraded = true
			continue
		}
		if platform.Int(stock, "available") > 0 {
			sellable = append(sellable, p)
		}
	}
	if ctx.Err() != nil {
		return nil, nil, status.FromContextError(ctx.Err()).Err()
	}
	items := rankProducts(sellable, profile, contextProduct, contextOrder, metadata, in.Scene, in.Limit, time.Now())
	if ctx.Err() != nil {
		return nil, nil, status.FromContextError(ctx.Err()).Err()
	}
	selectedStock := []map[string]any{}
	for _, item := range items {
		if stock, ok := stocks[platform.Text(item, "id")]; ok {
			selectedStock = append(selectedStock, stock)
		}
	}
	out := platform.Result{
		"items":              items,
		"inventory_items":    selectedStock,
		"strategy":           strategy,
		"personalized":       profile.count > 0,
		"personalized_model": false,
		"degraded":           degraded,
		"signal_count":       profile.count,
		"scene":              in.Scene,
	}
	ids := make([]string, 0, len(stocks))
	for id := range stocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	snapshot := []map[string]any{}
	for _, id := range ids {
		snapshot = append(snapshot, stocks[id])
	}
	return out, snapshot, nil
}

func validID(id string) bool {
	return len(id) <= 150 && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "\x00\r\n\t")
}
func rowsOf(value any) []map[string]any {
	var result struct {
		Items []map[string]any `json:"items"`
	}
	_ = platform.Decode(map[string]any{"items": value}, &result)
	return result.Items
}

func (s *Service) peerRows(
	ctx context.Context,
	operation platform.Operation,
	c platform.Request,
) ([]map[string]any, error) {
	result, err := operation(ctx, c)
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err = platform.Decode(result, &out); err != nil {
		return nil, status.Error(codes.Internal, "invalid recommendation dependency response")
	}
	return out.Items, nil
}
func (s *Service) readSignals(ctx context.Context, c platform.Request) (signalRows, bool) {
	ctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	var out signalRows
	var degraded bool
	var mu sync.Mutex
	var wg sync.WaitGroup
	clients := platform.NewClients(s.rpc)
	reads := []struct {
		operation   platform.Operation
		data        map[string]any
		destination *[]map[string]any
	}{
		{
			clients.Engagement.List,
			map[string]any{"kind": constants.RecommendationSignalFavorite, "limit": 100},
			&out.Favorite,
		},
		{
			clients.Engagement.List,
			map[string]any{"kind": constants.RecommendationSignalHistory, "limit": 100},
			&out.History,
		},
		{
			clients.Engagement.List,
			map[string]any{"kind": constants.RecommendationSignalCompare, "limit": 100},
			&out.Compare,
		},
		{
			clients.Engagement.List,
			map[string]any{"kind": constants.RecommendationSignalSearch, "limit": 100},
			&out.Search,
		},
		{clients.Order.List, map[string]any{"limit": 100}, &out.Orders},
	}
	for _, read := range reads {
		read := read
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := s.peerRows(
				ctx,
				read.operation,
				platform.Request{UserID: c.UserID, Role: constants.RoleUser, Data: read.data},
			)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				degraded = true
				return
			}
			*read.destination = rows
		}()
	}
	wg.Wait()
	return out, degraded
}

func ownedSignals(in signalRows, user string) (signalRows, bool) {
	foreign := false
	filter := func(rows []map[string]any) []map[string]any {
		out := []map[string]any{}
		rows = append([]map[string]any(nil), rows...)
		sort.SliceStable(rows, func(i, j int) bool { return signalTime(rows[i]).After(signalTime(rows[j])) })
		for _, row := range rows {
			if owner := platform.Text(row, "user_id"); owner != user {
				foreign = true
				continue
			}
			if len(out) == 100 {
				break
			}
			out = append(out, row)
		}
		return out
	}
	return signalRows{
		Favorite: filter(in.Favorite),
		History:  filter(in.History),
		Compare:  filter(in.Compare),
		Search:   filter(in.Search),
		Orders:   filter(in.Orders),
	}, foreign
}

func seedIDs(signals signalRows, order map[string]any, productID string) []string {
	out, seen := []string{}, map[string]bool{}
	add := func(id string) {
		if id != "" && validID(id) && !seen[id] && len(out) < 200 {
			seen[id] = true
			out = append(out, id)
		}
	}
	add(productID)
	for _, row := range rowsOf(order["items"]) {
		add(platform.Text(row, "sku_id"))
	}
	for _, row := range signals.Orders {
		if paidOrder(row) {
			for _, line := range rowsOf(row["items"]) {
				add(platform.Text(line, "sku_id"))
			}
		}
	}
	for _, rows := range [][]map[string]any{signals.Favorite, signals.Compare, signals.History} {
		for _, row := range rows {
			add(platform.Text(row, "sku_id"))
		}
	}
	return out
}

// Parent presentation includes every variant, so historical purchases remain
// excluded even when their option metadata falls beyond the bounded seed read.
func excludedProduct(p product, excluded map[string]bool) bool {
	if excluded[p.id] || excluded[p.rawID] {
		return true
	}
	attributes := platform.Map(p.raw["attributes"])
	for _, value := range []any{p.raw["variants"], attributes["variants"]} {
		for _, variant := range rowsOf(value) {
			sku, id := platform.Text(variant, "sku_id"), platform.Text(variant, "id")
			if sku != "" && excluded[sku] || id != "" && excluded[id] {
				return true
			}
		}
	}
	return false
}

func (s *Service) seed(ctx context.Context) error {
	r := record(
		"recommendation-policy",
		"",
		constants.RecommendationKindPolicy,
		constants.RecommendationStatusActive,
		map[string]any{"strategy": strategy, "personalized_model": false},
	)
	_, err := s.repo.InsertOnce(ctx, &r, nil)
	return err
}
func (s *Service) event(context.Context, platform.Event) error { return nil }
