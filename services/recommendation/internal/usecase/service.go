package usecase

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const strategy = "hybrid_content_v2"

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

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "recommendation.usecase."+c.Action)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	internal := c.Action == "rank_bootstrap"
	if c.Action != "list" && !internal {
		return nil, status.Error(codes.Unimplemented, "unsupported recommendation action")
	}
	if internal && c.Role != "system" {
		return nil, status.Error(codes.PermissionDenied, "bootstrap ranking is internal")
	}
	var in recommendationRequest
	if err := platform.Decode(c.Data, &in); err != nil {
		return nil, bad("invalid recommendation request")
	}
	if in.Scene == "" {
		in.Scene = "home"
		if in.OrderID != "" {
			in.Scene = "post_purchase"
		}
	}
	if in.Scene != "home" && in.Scene != "similar" && in.Scene != "post_purchase" {
		return nil, bad("scene must be home, similar or post_purchase")
	}
	if in.Limit == 0 {
		in.Limit = 4
	}
	if in.Limit < 1 || in.Limit > 20 {
		return nil, bad("limit must be between 1 and 20")
	}
	if !validID(in.ProductID) || !validID(in.OrderID) {
		return nil, bad("invalid product or order ID")
	}
	if in.Scene == "similar" && in.ProductID == "" {
		return nil, bad("similar recommendations require product_id")
	}
	if in.Scene == "post_purchase" && in.OrderID == "" {
		return nil, bad("post_purchase recommendations require order_id")
	}
	if internal && (in.Scene != "home" || in.OrderID != "" || in.ProductID != "") {
		return nil, bad("bootstrap ranking only accepts home scene")
	}
	if _, err := s.repo.Get(ctx, "recommendation-policy"); err != nil {
		return nil, err
	}
	var base, stockRows []map[string]any
	var signals signalRows
	degraded := false
	if internal {
		var input bootstrapInput
		if err := platform.Decode(c.Data, &input); err != nil || len(input.Catalog) > 500 || len(input.Inventory) > 1000 {
			return nil, bad("invalid bootstrap recommendation input")
		}
		base, stockRows, signals = input.Catalog, input.Inventory, input.Signals
		degraded = len(input.Errors) > 0
	} else {
		// Client data cannot supply or choose another principal's profile.
		var err error
		base, err = s.catalogRows(ctx, "list", map[string]any{"limit": 100})
		if err != nil {
			return nil, err
		}
		if c.UserID != "" {
			signals, degraded = s.readSignals(ctx, c)
		}
	}
	if c.UserID == "" {
		signals = signalRows{}
	}
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	signals, foreign := ownedSignals(signals, c.UserID)
	degraded = degraded || foreign
	contextOrder := map[string]any{}
	if in.OrderID != "" {
		if err := platform.RequireUser(c); err != nil {
			return nil, err
		}
		result, err := call(ctx, s.rpc, "order", platform.Command{Action: "get", ID: in.OrderID, UserID: c.UserID, Role: "user"})
		if err != nil {
			return nil, err
		}
		if platform.Text(result, "user_id") != c.UserID {
			return nil, status.Error(codes.PermissionDenied, "order belongs to another user")
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
		rows, err := s.catalogRows(ctx, "batch_get", map[string]any{"ids": missing})
		if err != nil {
			if in.ProductID != "" {
				return nil, err
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
		rows, err := s.catalogRows(ctx, "batch_get", map[string]any{"ids": parents})
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
			return nil, status.Error(codes.NotFound, "recommendation product unavailable")
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
		recall, err := call(ctx, s.rpc, "catalog", platform.Command{Action: "recommendation_candidates", Role: "system", Data: map[string]any{"categories": categories, "brands": brands, "queries": queries, "limit": 200}})
		if err != nil {
			if ctx.Err() != nil {
				return nil, status.FromContextError(ctx.Err()).Err()
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
		rows, err := s.peerRows(ctx, "inventory", platform.Command{Action: "batch_get", Data: map[string]any{"ids": stockIDs}})
		if err != nil {
			if !internal || len(stocks) == 0 {
				return nil, err
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
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	items := rankProducts(sellable, profile, contextProduct, contextOrder, metadata, in.Scene, in.Limit, time.Now())
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	selectedStock := []map[string]any{}
	for _, item := range items {
		if stock, ok := stocks[platform.Text(item, "id")]; ok {
			selectedStock = append(selectedStock, stock)
		}
	}
	out := platform.Result{"items": items, "inventory_items": selectedStock, "strategy": strategy, "personalized": profile.count > 0, "personalized_model": false, "degraded": degraded, "signal_count": profile.count, "scene": in.Scene}
	if internal {
		ids := make([]string, 0, len(stocks))
		for id := range stocks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		snapshot := []map[string]any{}
		for _, id := range ids {
			snapshot = append(snapshot, stocks[id])
		}
		out["stock_snapshot"] = snapshot
	}
	return out, nil
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
func (s *Service) peerRows(ctx context.Context, target string, c platform.Command) ([]map[string]any, error) {
	result, err := call(ctx, s.rpc, target, c)
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
func (s *Service) catalogRows(ctx context.Context, action string, data map[string]any) ([]map[string]any, error) {
	return s.peerRows(ctx, "catalog", platform.Command{Action: action, Role: "system", Data: data})
}

func (s *Service) readSignals(ctx context.Context, c platform.Command) (signalRows, bool) {
	ctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	var out signalRows
	var degraded bool
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, kind := range []string{"favorite", "history", "compare", "search", "orders"} {
		kind := kind
		wg.Add(1)
		go func() {
			defer wg.Done()
			target, data := "engagement", map[string]any{"kind": kind, "limit": 100}
			if kind == "orders" {
				target = "order"
				data = map[string]any{"limit": 100}
			}
			rows, err := s.peerRows(ctx, target, platform.Command{Action: "list", UserID: c.UserID, Role: "user", Data: data})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				degraded = true
				return
			}
			switch kind {
			case "favorite":
				out.Favorite = rows
			case "history":
				out.History = rows
			case "compare":
				out.Compare = rows
			case "search":
				out.Search = rows
			case "orders":
				out.Orders = rows
			}
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
	return signalRows{Favorite: filter(in.Favorite), History: filter(in.History), Compare: filter(in.Compare), Search: filter(in.Search), Orders: filter(in.Orders)}, foreign
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
	r := record("recommendation-policy", "", "policy", "ACTIVE", map[string]any{"strategy": strategy, "personalized_model": false})
	_, err := s.repo.InsertOnce(ctx, &r, nil)
	return err
}
func (s *Service) event(context.Context, platform.Event) error { return nil }
