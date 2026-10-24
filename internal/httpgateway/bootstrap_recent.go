package httpgateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/davveo/lumo/internal/platform"
)

const bootstrapRecentLimit = 6

// The side rail consumes the same public get commands as other storefront
// views. Prime those commands from verified, already-read account signals;
// unknown metadata and stock still use bounded SQL/RPC batches.
func (g *gateway) bootstrapRecent(ctx context.Context, fragments []platform.Result, principal platform.Command) []platform.Result {
	if principal.UserID == "" {
		return fragments
	}
	signals := map[string][]platform.Result{"history": {}, "favorite": {}}
	products, stocks := map[string]platform.Result{}, map[string]platform.Result{}
	seen := map[string]bool{}
	rememberProduct := func(row platform.Result) {
		if id := platform.Text(row, "id"); id != "" {
			// Recommendation annotations do not belong to a catalog/get snapshot.
			copy := platform.Result{}
			for key, value := range row {
				if key != "recommendation_reason" && key != "recommendation_reasons" && key != "recommendation_score" {
					copy[key] = value
				}
			}
			products[id] = copy
		}
	}
	remember := func(rows []platform.Result) {
		for _, fragment := range rows {
			service, action := platform.Text(fragment, "service"), platform.Text(fragment, "action")
			command := platform.Map(fragment["command"])
			request := readRequest{Service: service, Action: action, Command: readCommand{ID: platform.Text(command, "id"), Data: platform.Map(command["data"])}, auth: fragment["auth"] == true}
			seen[canonicalRead(request)] = true
			if fragment["data"] == nil || fragment["error"] != nil {
				continue
			}
			data := platform.Map(fragment["data"])
			if service == "engagement" && action == "list" && request.auth {
				kind := platform.Text(request.Command.Data, "kind")
				if _, wanted := signals[kind]; wanted {
					for _, row := range resultRows(data) {
						if platform.Text(row, "user_id") == principal.UserID {
							signals[kind] = append(signals[kind], row)
						}
					}
				}
			}
			if service == "catalog" && !request.auth {
				if action == "get" {
					rememberProduct(data)
				} else if action == "list" {
					for _, row := range resultRows(data) {
						rememberProduct(row)
					}
				}
			}
			if service == "inventory" && action == "get" && !request.auth {
				stocks[platform.Text(data, "sku_id")] = data
			}
			if service == "recommendation" && action == "list" {
				for _, row := range resultRows(data) {
					rememberProduct(row)
				}
				for _, row := range resultRows(platform.Result{"items": data["inventory_items"]}) {
					stocks[platform.Text(row, "sku_id")] = row
				}
			}
		}
	}
	remember(fragments)
	ids, selected := []string{}, map[string]bool{}
	for _, kind := range []string{"history", "favorite"} {
		for _, row := range signals[kind] {
			id := platform.Text(row, "sku_id")
			if id == "" || len(id) > 150 || strings.TrimSpace(id) != id || selected[id] || len(ids) == bootstrapRecentLimit {
				continue
			}
			selected[id] = true
			ids = append(ids, id)
		}
	}
	prime := func(service string, ids []string) {
		reads := []readRequest{}
		for _, id := range ids {
			request := readRequest{Key: fmt.Sprintf("bootstrap-recent-%s-%d", service, len(fragments)+len(reads)), Service: service, Action: "get", Command: readCommand{ID: id, Data: map[string]any{}}}
			key := canonicalRead(request)
			if id == "" || seen[key] {
				continue
			}
			seen[key] = true
			known := products[id]
			if service == "inventory" {
				known = stocks[id]
			}
			if known != nil {
				fragments = append(fragments, readResult(request, known, nil))
			} else {
				reads = append(reads, request)
			}
		}
		loaded := g.runReads(ctx, reads, principal)
		fragments = append(fragments, loaded...)
		remember(loaded)
	}
	// A variant get remains the real variant. Resolve its parent separately so
	// the rail can show the public product with its complete default metadata.
	prime("catalog", ids)
	parents := []string{}
	parentSeen := map[string]bool{}
	for _, id := range ids {
		row := products[id]
		if row == nil {
			continue
		}
		parent := recentParent(row)
		if parent != "" && len(parent) <= 150 && strings.TrimSpace(parent) == parent && !parentSeen[parent] {
			parentSeen[parent] = true
			parents = append(parents, parent)
		}
	}
	prime("catalog", parents)
	defaults := []string{}
	for _, id := range parents {
		if product := products[id]; product != nil {
			defaults = append(defaults, platform.Text(product, "id"))
		}
	}
	prime("inventory", defaults)
	return fragments
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
