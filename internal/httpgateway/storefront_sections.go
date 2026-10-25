package httpgateway

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Each section has a fixed server-owned read plan. Callers cannot submit a
// service/action list or make an unrelated private dependency block the shell.
func (g *gateway) storefrontSection(ctx context.Context, c platform.Command) (platform.Result, error) {
	if c.ID != "" || len(c.Data) != 0 {
		return nil, status.Error(codes.InvalidArgument, "section does not accept read commands")
	}
	switch c.Action {
	case constants.ActionShell, constants.ActionBootstrap:
		return g.storefrontShell(ctx)
	case constants.ActionProducts:
		return g.storefrontProducts(ctx)
	case constants.ActionActivities:
		return g.storefrontActivities(ctx, c)
	case constants.ActionAccountSummary:
		if err := platform.RequireUser(c); err != nil {
			return nil, err
		}
		return g.storefrontAccountSummary(ctx, c), nil
	}
	return nil, status.Error(codes.NotFound, "storefront section not found")
}
func (g *gateway) storefrontShell(ctx context.Context) (platform.Result, error) {
	plan := []readRequest{
		sectionRead("settings", constants.ServiceContent, constants.ActionGet, "storefront-main", nil, false),
		sectionRead("navigation", constants.ServiceContent, constants.ActionGet, storefrontCategoriesID, nil, false),
	}
	rows := g.runReads(ctx, plan, platform.Command{})
	for _, row := range rows {
		if row["key"] == "settings" && platform.Text(platform.Map(row["error"]), "code") == codes.NotFound.String() {
			row["optional"] = true
		}
	}
	return platform.Result{"results": rows}, nil
}
func (g *gateway) storefrontProducts(ctx context.Context) (platform.Result, error) {
	plan := []readRequest{
		sectionRead(
			"products",
			constants.ServiceCatalog,
			constants.ActionList,
			"",
			map[string]any{"limit": 100},
			false,
		),
	}
	rows := g.runReads(ctx, plan, platform.Command{})
	return platform.Result{"results": g.sectionStocks(ctx, rows)}, nil
}
func (g *gateway) storefrontActivities(ctx context.Context, c platform.Command) (platform.Result, error) {
	plan := []readRequest{
		sectionRead("campaigns", constants.ServiceCampaign, constants.ActionList, "", nil, false),
		sectionRead("flash", constants.ServiceFlash, constants.ActionList, "", nil, false),
		sectionRead("groups", constants.ServiceGroup, constants.ActionList, "", nil, false),
		sectionRead(
			"coupon-preview",
			constants.ServiceCoupon,
			constants.ActionTemplates,
			"",
			map[string]any{"page": 1, "page_size": 2},
			c.UserID != "",
		),
	}
	rows := g.runReads(ctx, plan, c)
	ids := map[string]bool{}
	for _, row := range rows {
		if row["service"] != constants.ServiceFlash && row["service"] != constants.ServiceGroup {
			continue
		}
		for _, offer := range resultRows(platform.Map(row["data"])) {
			if id := platform.Text(offer, "sku_id"); id != "" {
				ids[id] = true
			}
		}
	}
	if len(ids) > 0 {
		sorted := make([]string, 0, len(ids))
		for id := range ids {
			sorted = append(sorted, id)
		}
		sort.Strings(sorted)
		for first := 0; first < len(sorted); first += 500 {
			chunk := sorted[first:min(first+500, len(sorted))]
			rows = append(rows, g.runReads(ctx, []readRequest{
				sectionRead(
					fmt.Sprintf("offer-products-%d", first),
					constants.ServiceCatalog,
					constants.ActionBatchGet,
					"",
					map[string]any{"ids": chunk},
					false,
				),
				sectionRead(
					fmt.Sprintf("offer-stock-%d", first),
					constants.ServiceInventory,
					constants.ActionBatchGet,
					"",
					map[string]any{"ids": chunk},
					false,
				),
			}, platform.Command{})...)
		}
	}
	return platform.Result{"results": rows}, nil
}
func sectionRead(key, service, action, id string, data map[string]any, auth bool) readRequest {
	if data == nil {
		data = map[string]any{}
	}
	return readRequest{Key: key, Service: service, Action: action, Command: readCommand{ID: id, Data: data}, auth: auth}
}
func (g *gateway) sectionStocks(ctx context.Context, rows []platform.Result) []platform.Result {
	ids := map[string]bool{}
	for _, fragment := range rows {
		if fragment["service"] != constants.ServiceCatalog || fragment["error"] != nil {
			continue
		}
		for _, p := range resultRows(platform.Map(fragment["data"])) {
			if id := platform.Text(p, "id"); id != "" {
				ids[id] = true
			}
			for _, candidates := range []any{p["variants"], platform.Map(p["attributes"])["variants"]} {
				for _, variant := range resultRows(platform.Result{"items": candidates}) {
					id := platform.Text(variant, "sku_id")
					if id == "" {
						id = platform.Text(variant, "id")
					}
					if id != "" {
						ids[id] = true
					}
				}
			}
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for first := 0; first < len(ordered); first += 500 {
		chunk := ordered[first:min(first+500, len(ordered))]
		rows = append(
			rows,
			g.runReads(
				ctx,
				[]readRequest{
					sectionRead(
						fmt.Sprintf("product-stock-%d", first),
						constants.ServiceInventory,
						constants.ActionBatchGet,
						"",
						map[string]any{"ids": chunk},
						false,
					),
				},
				platform.Command{},
			)...)
	}
	return rows
}
func (g *gateway) storefrontAccountSummary(ctx context.Context, c platform.Command) platform.Result {
	reads := []readRequest{
		sectionRead("profile", constants.ServiceMember, constants.ActionGet, "", nil, true),
		sectionRead("points", constants.ServicePoints, constants.ActionGet, "", nil, true),
		sectionRead("coupons", constants.ServiceCoupon, constants.ActionList, "", nil, true),
		sectionRead(
			"favorites",
			constants.ServiceEngagement,
			constants.ActionList,
			"",
			map[string]any{"kind": constants.EngagementKindFavorite, "limit": 100},
			true,
		),
		sectionRead("cart", constants.ServiceCart, constants.ActionList, "", nil, true),
		sectionRead(
			"messages",
			constants.ServiceNotification,
			constants.ActionList,
			"",
			map[string]any{"limit": 100},
			true,
		),
	}
	summary := platform.Result{
		"profile":        nil,
		"points":         nil,
		"coupon_count":   nil,
		"favorite_count": nil,
		"favorite_ids":   nil,
		"cart_count":     nil,
		"unread_count":   nil,
	}
	failures := platform.Result{}
	for _, fragment := range g.runReads(ctx, reads, c) {
		key := platform.Text(fragment, "key")
		if fragment["error"] != nil {
			failures[key] = fragment["error"]
			continue
		}
		data := platform.Map(fragment["data"])
		switch key {
		case "profile":
			summary["profile"] = platform.Result{
				"display_name": data["display_name"],
				"level":        data["level"],
				"avatar":       data["avatar"],
			}
		case "points":
			summary["points"] = data["available"]
		case "coupons":
			count := 0
			for _, coupon := range resultRows(data) {
				if platform.Text(coupon, "status") != constants.CouponStatusAvailable {
					continue
				}
				expiry, err := time.Parse(time.RFC3339, platform.Text(coupon, "expires_at"))
				if err == nil && expiry.After(time.Now()) {
					count++
				}
			}
			summary["coupon_count"] = count
		case "favorites":
			ids := []string{}
			for _, row := range resultRows(data) {
				if id := platform.Text(row, "sku_id"); id != "" {
					ids = append(ids, id)
				}
			}
			summary["favorite_ids"] = ids
			summary["favorite_count"] = len(ids)
			if _, ok := data["total"]; ok {
				summary["favorite_count"] = max(0, platform.Int(data, "total"))
			}
		case "cart":
			count := 0
			for _, row := range resultRows(data) {
				count += int(max(0, platform.Int(row, "quantity")))
			}
			summary["cart_count"] = count
			if _, ok := data["quantity_total"]; ok {
				summary["cart_count"] = max(0, platform.Int(data, "quantity_total"))
			}
		case "messages":
			count := 0
			for _, row := range resultRows(data) {
				if platform.Text(row, "status") != constants.NotificationStatusRead {
					count++
				}
			}
			summary["unread_count"] = count
			if _, ok := data["unread_total"]; ok {
				summary["unread_count"] = max(0, platform.Int(data, "unread_total"))
			}
		}
	}
	return platform.Result{"summary": summary, "errors": failures}
}
