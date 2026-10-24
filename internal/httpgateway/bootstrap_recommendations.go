package httpgateway

import (
	"context"
	"fmt"
	"sort"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Bootstrap has already read the caller's signals and the public catalog. Pass
// that verified snapshot to the internal ranker rather than fetching the same
// lists again inside the recommendation service. The HTTP whitelist never
// exposes rank_bootstrap or accepts a nested principal/profile from the client.
func (g *gateway) bootstrapRecommendations(
	ctx context.Context,
	fragments []platform.Result,
	principal platform.Command,
	stockVariants ...bool,
) []platform.Result {
	query := readRequest{
		Key:     "bootstrap-recommendation",
		Service: constants.ServiceRecommendation,
		Action:  constants.ActionList,
		Command: readCommand{Data: map[string]any{"scene": constants.RecommendationSceneHome, "limit": 20}},
		auth:    principal.UserID != "",
	}
	signals := map[string]any{}
	for _, kind := range []string{
		constants.RecommendationSignalFavorite,
		constants.RecommendationSignalHistory,
		constants.RecommendationSignalCompare,
		constants.RecommendationSignalSearch,
		constants.RecommendationSignalOrders,
	} {
		signals[kind] = []platform.Result{}
	}
	catalog, stocks := []platform.Result{}, []platform.Result{}
	known := map[string]bool{}
	signalErrors := []string{}
	catalogOK := false
	for _, fragment := range fragments {
		service, action := platform.Text(fragment, "service"), platform.Text(fragment, "action")
		command := platform.Map(fragment["command"])
		kind := platform.Text(platform.Map(command["data"]), "kind")
		data, success := fragment["data"].(platform.Result)
		if service == constants.ServiceCatalog && action == constants.ActionList {
			catalogOK = success
			if success {
				catalog = resultRows(data)
			}
		}
		if service == constants.ServiceInventory && action == constants.ActionGet && success {
			stocks = append(stocks, data)
			known[platform.Text(data, "sku_id")] = true
		}
		if principal.UserID == "" || fragment["auth"] != true {
			continue
		}
		if service == constants.ServiceOrder && action == constants.ActionList {
			kind = constants.RecommendationSignalOrders
		} else if service != constants.ServiceEngagement || action != constants.ActionList {
			continue
		}
		if _, expected := signals[kind]; !expected {
			continue
		}
		if success {
			rows := resultRows(data)
			owned := []platform.Result{}
			for _, row := range rows {
				if platform.Text(row, "user_id") == principal.UserID {
					owned = append(owned, row)
				}
			}
			if len(owned) != len(rows) {
				signalErrors = append(signalErrors, kind)
			}
			rows = owned
			signals[kind] = rows
		} else {
			signalErrors = append(signalErrors, kind)
		}
	}
	if !catalogOK {
		return append(
			fragments,
			readResult(query, nil, status.Error(codes.Unavailable, "recommendation catalog unavailable")),
		)
	}
	// Ranking only needs each candidate's default SKU. Avoid forwarding every
	// option/cart stock row when a catalog has many independently stocked variants.
	defaults := map[string]bool{}
	for _, product := range catalog {
		defaults[platform.Text(product, "id")] = true
	}
	rankingStocks := []platform.Result{}
	for _, stock := range stocks {
		if defaults[platform.Text(stock, "sku_id")] {
			rankingStocks = append(rankingStocks, stock)
		}
	}
	data, err := g.readRPC(ctx, constants.ServiceRecommendation, platform.Command{
		Action: constants.ActionRankBootstrap, UserID: principal.UserID, Role: constants.RoleSystem,
		Data: map[string]any{
			"scene":           constants.RecommendationSceneHome,
			"limit":           20,
			"catalog_items":   catalog,
			"inventory_items": rankingStocks,
			"signals":         signals,
			"signal_errors":   signalErrors,
		},
	})
	if err != nil {
		return append(fragments, readResult(query, nil, err))
	}
	public := platform.Result{}
	for key, value := range data {
		if key != "stock_snapshot" {
			public[key] = value
		}
	}
	fragments = append(fragments, readResult(query, public, nil))
	available := map[string]platform.Result{}
	for _, stock := range resultRows(platform.Result{"items": data["stock_snapshot"]}) {
		available[platform.Text(stock, "sku_id")] = stock
	}
	needed := map[string]bool{}
	for _, product := range resultRows(public) {
		if id := platform.Text(product, "id"); id != "" {
			needed[id] = true
		}
		// The dedicated recent page only displays default SKUs; bootstrap also
		// primes variant pickers used by the rest of the storefront.
		if len(stockVariants) != 0 && !stockVariants[0] {
			continue
		}
		attributes := platform.Map(product["attributes"])
		for _, variants := range []any{product["variants"], attributes["variants"]} {
			for _, variant := range resultRows(platform.Result{"items": variants}) {
				id := platform.Text(variant, "sku_id")
				if id == "" {
					id = platform.Text(variant, "id")
				}
				if id != "" {
					needed[id] = true
				}
			}
		}
	}
	ids := make([]string, 0, len(needed))
	for id := range needed {
		if !known[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	reads := []readRequest{}
	for i, id := range ids {
		item := readRequest{
			Key:     fmt.Sprintf("bootstrap-recommendation-stock-%d", i),
			Service: constants.ServiceInventory,
			Action:  constants.ActionGet,
			Command: readCommand{ID: id, Data: map[string]any{}},
		}
		if stock, exists := available[id]; exists {
			fragments = append(fragments, readResult(item, stock, nil))
		} else {
			reads = append(reads, item)
		}
	}
	return append(fragments, g.runReads(ctx, reads, principal)...)
}
