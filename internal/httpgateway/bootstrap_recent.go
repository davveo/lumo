package httpgateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const bootstrapRecentLimit = 6

// recent is a page read, not a client-controlled recommendation/profile query.
// It uses the same verified snapshot and ranker as bootstrap, without fetching
// the rest of the account page or repeating engagement/order reads in ranking.
func (g *gateway) storefrontRecent(ctx context.Context, principal platform.Command) (platform.Result, error) {
	if principal.ID != "" || len(principal.Data) != 0 {
		return nil, status.Error(codes.InvalidArgument, "recent does not accept parameters")
	}
	plan := []readRequest{{Key: "recent-catalog", Service: constants.ServiceCatalog, Action: constants.ActionList, Command: readCommand{Data: map[string]any{"limit": 100}}}}
	if principal.UserID != "" {
		for _, kind := range []string{constants.EngagementKindHistory, constants.EngagementKindFavorite, constants.EngagementKindCompare, constants.EngagementKindSearch} {
			plan = append(plan, readRequest{Key: "recent-" + kind, Service: constants.ServiceEngagement, Action: constants.ActionList, Command: readCommand{Data: map[string]any{"kind": kind, "limit": 100}}, auth: true})
		}
		plan = append(plan, readRequest{Key: "recent-orders", Service: constants.ServiceOrder, Action: constants.ActionList, Command: readCommand{Data: map[string]any{}}, auth: true})
	}
	fragments := g.runReads(ctx, plan, principal)
	stockReads := []readRequest{}
	for _, fragment := range fragments {
		if fragment["service"] == constants.ServiceCatalog && fragment["error"] == nil {
			for _, row := range resultRows(platform.Map(fragment["data"])) {
				if id := platform.Text(row, "id"); recentID(id) {
					stockReads = append(stockReads, readRequest{Key: fmt.Sprintf("recent-stock-%d", len(stockReads)), Service: constants.ServiceInventory, Action: constants.ActionGet, Command: readCommand{ID: id, Data: map[string]any{}}})
				}
			}
		}
	}
	fragments = append(fragments, g.runReads(ctx, stockReads, principal)...)
	fragments = g.bootstrapRecommendations(ctx, fragments, principal, false)
	fragments = g.bootstrapRecent(ctx, fragments, principal)
	last := fragments[len(fragments)-1]
	if last["error"] != nil {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return nil, status.Error(codes.Unavailable, "recent catalog unavailable")
	}
	return platform.Map(last["data"]), nil
}

type recentSnapshot struct {
	products, stocks                         map[string]platform.Result
	aliases                                  map[string]string
	seen                                     map[string]bool
	history, favorites, recommended, catalog []platform.Result
	catalogOK, recommendationOK              bool
	personalized, degraded                   bool
	inventoryFailed                          bool
	offset, total                            int
	exhausted                                bool
}

func newRecentSnapshot() *recentSnapshot {
	return &recentSnapshot{products: map[string]platform.Result{}, stocks: map[string]platform.Result{}, aliases: map[string]string{}, seen: map[string]bool{}}
}

func (s *recentSnapshot) product(row platform.Result) {
	id := platform.Text(row, "id")
	if !recentID(id) {
		return
	}
	// A ranked copy can carry explanations, but must not replace authoritative
	// catalog metadata (particularly enabled state) already in this snapshot.
	if s.products[id] == nil {
		s.products[id] = row
	}
	parent := recentParent(row)
	if recentID(parent) {
		s.aliases[id] = parent
	}
	if parent != id {
		return
	}
	attributes := platform.Map(row["attributes"])
	for _, value := range []any{row["variants"], attributes["variants"]} {
		for _, variant := range resultRows(platform.Result{"items": value}) {
			variantID := platform.Text(variant, "sku_id")
			if variantID == "" {
				variantID = platform.Text(variant, "id")
			}
			if recentID(variantID) {
				s.aliases[variantID] = id
			}
		}
	}
}

func (s *recentSnapshot) remember(fragments []platform.Result, principal platform.Command) {
	for _, fragment := range fragments {
		service, action := platform.Text(fragment, "service"), platform.Text(fragment, "action")
		command := platform.Map(fragment["command"])
		request := readRequest{Service: service, Action: action, Command: readCommand{ID: platform.Text(command, "id"), Data: platform.Map(command["data"])}, auth: fragment["auth"] == true}
		s.seen[canonicalRead(request)] = true
		failed := fragment["error"] != nil || fragment["data"] == nil
		data := platform.Map(fragment["data"])
		if service == constants.ServiceCatalog && !request.auth {
			if failed {
				// A deleted historical SKU is an expected miss, not an outage.
				if platform.Int(platform.Map(fragment["error"]), "status") != 404 {
					s.degraded = true
				}
				continue
			}
			if action == constants.ActionGet {
				s.product(data)
			}
			if action == constants.ActionList {
				rows := resultRows(data)
				s.catalogOK = true
				s.catalog = append(s.catalog, rows...)
				for _, row := range rows {
					s.product(row)
				}
				limit := int(platform.Int(request.Command.Data, "limit"))
				if limit < 1 {
					limit = 20
				}
				s.offset = int(platform.Int(request.Command.Data, "offset")) + len(rows)
				s.total = int(platform.Int(data, "total"))
				_, hasTotal := data["total"]
				s.exhausted = len(rows) < limit || (hasTotal && s.offset >= s.total)
			}
		}
		if service == constants.ServiceInventory && action == constants.ActionGet && !request.auth {
			if failed {
				s.degraded = true
				s.inventoryFailed = s.inventoryFailed || platform.Int(platform.Map(fragment["error"]), "status") != 404
			} else if id := platform.Text(data, "sku_id"); recentID(id) {
				s.stocks[id] = data
			}
		}
		if service == constants.ServiceRecommendation && action == constants.ActionList && request.auth == (principal.UserID != "") {
			if failed {
				s.degraded = true
				continue
			}
			s.recommendationOK = true
			s.recommended = resultRows(data)
			s.personalized = principal.UserID != "" && data["personalized"] == true
			s.degraded = s.degraded || data["degraded"] == true
			for _, row := range s.recommended {
				s.product(row)
			}
			for _, stock := range resultRows(platform.Result{"items": data["inventory_items"]}) {
				s.stocks[platform.Text(stock, "sku_id")] = stock
			}
		}
		if principal.UserID != "" && request.auth && ((service == constants.ServiceEngagement && action == constants.ActionList) || (service == constants.ServiceOrder && action == constants.ActionList)) {
			if failed {
				s.degraded = true
				continue
			}
			kind := platform.Text(request.Command.Data, "kind")
			for _, row := range resultRows(data) {
				if platform.Text(row, "user_id") != principal.UserID {
					continue
				}
				if kind == constants.EngagementKindHistory {
					s.history = append(s.history, row)
				}
				if kind == constants.EngagementKindFavorite {
					s.favorites = append(s.favorites, row)
				}
			}
		}
	}
}

func recentID(id string) bool {
	return id != "" && len(id) <= 150 && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "\x00\r\n\t")
}

func recentVisible(row platform.Result) bool {
	if row == nil || !recentID(platform.Text(row, "id")) || recentParent(row) != platform.Text(row, "id") {
		return false
	}
	attributes := platform.Map(row["attributes"])
	name := strings.TrimSpace(platform.Text(row, "name"))
	if name == "" {
		name = strings.TrimSpace(platform.Text(attributes, "name"))
	}
	price := platform.Int(row, "price_cents")
	if price <= 0 {
		price = platform.Int(attributes, "price_cents")
	}
	if name == "" || price <= 0 {
		return false
	}
	for _, source := range []platform.Result{row, attributes} {
		for _, key := range []string{"active", "catalog_visible"} {
			value, exists := source[key]
			if exists && (value == false || value == "false" || value == "0") {
				return false
			}
		}
		if state := platform.Text(source, "status"); state != "" && state != constants.CatalogStatusActive {
			return false
		}
	}
	return true
}

// bootstrapRecent builds the entire rail from already-read account/public data.
// Missing option/parent metadata and stock remain bounded bulk reads; no browser
// per-item hydration or second account-profile/order read is needed.
func (g *gateway) bootstrapRecent(ctx context.Context, fragments []platform.Result, principal platform.Command) []platform.Result {
	snapshot := newRecentSnapshot()
	snapshot.remember(fragments, principal)
	query := readRequest{Key: "bootstrap-recent", Service: constants.ServiceStorefront, Action: constants.ActionRecent, Command: readCommand{Data: map[string]any{}}, auth: principal.UserID != ""}
	if !snapshot.catalogOK && !snapshot.recommendationOK {
		return append(fragments, readResult(query, nil, status.Error(codes.Unavailable, "recent catalog unavailable")))
	}
	prime := func(service string, ids []string) {
		if service == constants.ServiceInventory && snapshot.inventoryFailed {
			return
		}
		reads := []readRequest{}
		for _, id := range ids {
			request := readRequest{Key: fmt.Sprintf("bootstrap-recent-%s-%d", service, len(fragments)+len(reads)), Service: service, Action: constants.ActionGet, Command: readCommand{ID: id, Data: map[string]any{}}}
			key := canonicalRead(request)
			if !recentID(id) || snapshot.seen[key] {
				continue
			}
			if (service == constants.ServiceCatalog && snapshot.products[id] != nil) || (service == constants.ServiceInventory && snapshot.stocks[id] != nil) {
				continue
			}
			snapshot.seen[key] = true
			reads = append(reads, request)
		}
		loaded := g.runReads(ctx, reads, principal)
		fragments = append(fragments, loaded...)
		snapshot.remember(loaded, principal)
	}
	// Variants already advertised by a real catalog parent need no SKU lookup.
	// Up to 100 rows of each owned signal kind were read in the original plan.
	unknown := []string{}
	for _, rows := range [][]platform.Result{snapshot.history, snapshot.favorites} {
		for _, row := range rows {
			if id := platform.Text(row, "sku_id"); recentID(id) && snapshot.aliases[id] == "" {
				unknown = append(unknown, id)
			}
		}
	}
	prime(constants.ServiceCatalog, unknown)
	parents := []string{}
	for _, id := range unknown {
		if parent := snapshot.aliases[id]; parent != "" {
			parents = append(parents, parent)
		}
	}
	prime(constants.ServiceCatalog, parents)
	type candidate struct {
		row    platform.Result
		source string
	}
	candidates, seen := []candidate{}, map[string]bool{}
	add := func(row platform.Result, source string) {
		id := platform.Text(row, "id")
		if !recentVisible(row) || seen[id] {
			return
		}
		seen[id] = true
		candidates = append(candidates, candidate{row, source})
	}
	for _, source := range []struct {
		kind string
		rows []platform.Result
	}{{constants.RecommendationSourceHistory, snapshot.history}, {constants.RecommendationSourceFavorite, snapshot.favorites}} {
		for _, signal := range source.rows {
			add(snapshot.products[snapshot.aliases[platform.Text(signal, "sku_id")]], source.kind)
		}
	}
	for _, row := range snapshot.recommended {
		id := platform.Text(row, "id")
		// Keep the ranker's explanations while validating authoritative catalog
		// state if it was already loaded, rather than resurrecting disabled data.
		if recentVisible(snapshot.products[id]) {
			add(row, constants.RecommendationSourceRecommendation)
		}
	}
	for _, row := range snapshot.catalog {
		add(row, constants.RecommendationSourceFeatured)
	}
	selected, inventory := []platform.Result{}, []platform.Result{}
	personalized := false
	consume := func(first int) {
		ids := []string{}
		for _, candidate := range candidates[first:] {
			ids = append(ids, platform.Text(candidate.row, "id"))
		}
		prime(constants.ServiceInventory, ids)
		for _, candidate := range candidates[first:] {
			id := platform.Text(candidate.row, "id")
			stock := snapshot.stocks[id]
			if len(selected) >= bootstrapRecentLimit || stock == nil || platform.Int(stock, "available") < 1 {
				continue
			}
			row := platform.Result{}
			for key, value := range candidate.row {
				row[key] = value
			}
			row["recent_source"] = candidate.source
			selected, inventory = append(selected, row), append(inventory, stock)
			personalized = personalized || candidate.source == constants.RecommendationSourceHistory || candidate.source == constants.RecommendationSourceFavorite || (candidate.source == constants.RecommendationSourceRecommendation && snapshot.personalized)
		}
	}
	consume(0)
	// A sparse/out-of-stock first page is not proof the catalog has <6 goods.
	// Only scan more real pages when needed, preserving the normal bootstrap's
	// single catalog read and inventory batch when its snapshot already suffices.
	for len(selected) < bootstrapRecentLimit && !snapshot.exhausted && snapshot.catalogOK && !snapshot.inventoryFailed && ctx.Err() == nil {
		before, offset, knownProducts := len(candidates), snapshot.offset, len(snapshot.products)
		request := readRequest{Key: fmt.Sprintf("bootstrap-recent-page-%d", offset), Service: constants.ServiceCatalog, Action: constants.ActionList, Command: readCommand{Data: map[string]any{"limit": 100, "offset": offset}}}
		loaded := g.runReads(ctx, []readRequest{request}, principal)
		fragments = append(fragments, loaded...)
		snapshot.remember(loaded, principal)
		if loaded[0]["error"] != nil || snapshot.offset <= offset {
			break
		}
		for _, row := range resultRows(platform.Map(loaded[0]["data"])) {
			add(row, constants.RecommendationSourceFeatured)
		}
		consume(before)
		if knownProducts == len(snapshot.products) && !snapshot.exhausted {
			// Stop a broken pagination dependency from returning the same page forever.
			snapshot.degraded = true
			break
		}
	}
	if ctx.Err() != nil {
		return append(fragments, readResult(query, nil, status.FromContextError(ctx.Err()).Err()))
	}
	data := platform.Result{"items": selected, "inventory_items": inventory, "personalized": personalized, "degraded": snapshot.degraded, "limit": bootstrapRecentLimit, "count": len(selected)}
	return append(fragments, readResult(query, data, nil))
}

func recentParent(row platform.Result) string {
	attributes := platform.Map(row["attributes"])
	for _, source := range []platform.Result{row, attributes} {
		for _, field := range []string{"product_id", "parent_id"} {
			if id := platform.Text(source, field); id != "" {
				return id
			}
		}
	}
	return platform.Text(row, "id")
}
