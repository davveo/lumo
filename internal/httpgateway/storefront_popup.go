package httpgateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const popupVisitorCookie = "lumo_popup_visitor"
const popupSessionCookie = "lumo_popup_session"

type popupIdentityContextKey struct{}
type popupAccountContextKey struct{}
type popupIdentity struct{ visitor, session string }

// These opaque identifiers are for anonymous delivery ownership and frequency
// only. Authenticated caps always bind to the validated account, never a cookie.
func preparePopupIdentity(ctx context.Context, w http.ResponseWriter, r *http.Request) context.Context {
	identity := popupIdentity{}
	for _, entry := range []struct {
		name       string
		target     *string
		persistent bool
	}{
		{popupVisitorCookie, &identity.visitor, true}, {popupSessionCookie, &identity.session, false},
	} {
		if cookie, err := r.Cookie(entry.name); err == nil && validPopupCookie(cookie.Value) {
			*entry.target = cookie.Value
			continue
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return ctx
		}
		value := hex.EncodeToString(raw)
		*entry.target = value
		cookie := &http.Cookie{
			Name:     entry.name,
			Value:    value,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
		}
		if entry.persistent {
			cookie.MaxAge = 365 * 24 * 60 * 60
			cookie.Expires = time.Now().UTC().AddDate(1, 0, 0)
		}
		http.SetCookie(w, cookie)
	}
	return context.WithValue(ctx, popupIdentityContextKey{}, identity)
}
func validPopupCookie(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func popupPlacement(data map[string]any) (string, error) {
	placement := platform.Text(data, "placement")
	if placement == "" {
		placement = constants.PopupPlacementHome
	}
	switch placement {
	case constants.PopupPlacementHome,
		constants.PopupPlacementCategory,
		constants.PopupPlacementProduct,
		constants.PopupPlacementCoupons,
		constants.PopupPlacementNone:
		return placement, nil
	}
	return "", status.Error(codes.InvalidArgument, "unsupported popup placement")
}

func popupCommand(
	ctx context.Context,
	principal platform.Command,
	action, placement string,
	profile platform.Result,
) (platform.Command, bool) {
	identity, ok := ctx.Value(popupIdentityContextKey{}).(popupIdentity)
	if !ok || !validPopupCookie(identity.visitor) || !validPopupCookie(identity.session) ||
		principal.Role == constants.RoleAdmin ||
		placement == constants.PopupPlacementNone {
		return platform.Command{}, false
	}
	data := map[string]any{"placement": placement, "visitor_id": identity.visitor, "session_id": identity.session}
	if profile != nil {
		data["profile"] = profile
	}
	return platform.Command{Action: action, UserID: principal.UserID, Role: constants.RoleSystem, Data: data}, true
}

// A bootstrap uses already fetched authoritative account fragments. It adds no
// profile/points/history calls and never puts private targeting rules in results.
func (g *gateway) bootstrapPopup(ctx context.Context, principal platform.Command, results []platform.Result) any {
	placement, err := popupPlacement(principal.Data)
	if err != nil {
		return nil
	}
	command, ok := popupCommand(
		ctx,
		principal,
		constants.ActionPopupDecide,
		placement,
		popupProfile(ctx, principal, results),
	)
	if !ok {
		return nil
	}
	return g.optionalPopupDecision(ctx, command)
}
func (g *gateway) optionalPopupDecision(ctx context.Context, command platform.Command) any {
	if ctx.Err() != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, err := g.rpc.Call(ctx, constants.ServiceContent, command)
	if err != nil {
		return nil
	} // Marketing failure cannot prevent shopping.
	return result["popup"]
}
func (g *gateway) storefrontPopup(ctx context.Context, principal platform.Command) (platform.Result, error) {
	allowed := map[string]bool{"placement": true}
	if principal.Action != constants.ActionPopup {
		allowed["delivery_id"] = true
		allowed["view_id"] = true
	}
	if principal.Action == constants.ActionPopupEvent {
		allowed["event"] = true
	}
	for field := range principal.Data {
		if !allowed[field] {
			return nil, status.Error(codes.InvalidArgument, "unsupported popup request field")
		}
	}
	placement, err := popupPlacement(principal.Data)
	if err != nil {
		return nil, err
	}
	action := map[string]string{
		constants.ActionPopup:      constants.ActionPopupDecide,
		constants.ActionPopupEvent: constants.ActionPopupEvent,
		constants.ActionPopupClaim: constants.ActionPopupClaim,
	}[principal.Action]
	command, ok := popupCommand(ctx, principal, action, placement, nil)
	if !ok {
		if principal.Action == constants.ActionPopup {
			return platform.Result{"popup": nil}, nil
		}
		return platform.Result{"allowed": false}, nil
	}
	if principal.Action == constants.ActionPopup {
		fragments := []platform.Result{}
		if principal.UserID != "" {
			plan := []readRequest{
				{
					Key:     "popup-member",
					Service: constants.ServiceMember,
					Action:  constants.ActionGet,
					Command: readCommand{Data: map[string]any{}},
					auth:    true,
				},
				{
					Key:     "popup-points",
					Service: constants.ServicePoints,
					Action:  constants.ActionGet,
					Command: readCommand{Data: map[string]any{}},
					auth:    true,
				},
				{
					Key:     "popup-history",
					Service: constants.ServiceEngagement,
					Action:  constants.ActionList,
					Command: readCommand{Data: map[string]any{"kind": constants.EngagementKindHistory, "limit": 100}},
					auth:    true,
				},
				{
					Key:     "popup-favorite",
					Service: constants.ServiceEngagement,
					Action:  constants.ActionList,
					Command: readCommand{Data: map[string]any{"kind": constants.EngagementKindFavorite, "limit": 100}},
					auth:    true,
				},
			}
			fragments = g.runReads(ctx, plan, principal)
			// Hydrate up to 50 interest products with one catalog batch, without stock reads.
			fragments = g.popupInterestProducts(ctx, fragments, principal)
		}
		command.Data["profile"] = popupProfile(ctx, principal, fragments)
		return platform.Result{"popup": g.optionalPopupDecision(ctx, command)}, nil
	}
	for _, field := range []string{"delivery_id", "view_id"} {
		value := platform.Text(principal.Data, field)
		if value == "" || len(value) > 150 || strings.TrimSpace(value) != value {
			return nil, status.Error(codes.InvalidArgument, field+" required")
		}
		command.Data[field] = value
	}
	if principal.Action == constants.ActionPopupEvent {
		event := platform.Text(principal.Data, "event")
		switch event {
		case constants.PopupEventImpression,
			constants.PopupEventClick,
			constants.PopupEventDismiss,
			constants.PopupEventAutoClose:
		default:
			return nil, status.Error(codes.InvalidArgument, "invalid popup event")
		}
		command.Data["event"] = event
	}
	return g.rpc.Call(ctx, constants.ServiceContent, command)
}

func popupProfile(ctx context.Context, principal platform.Command, fragments []platform.Result) platform.Result {
	if principal.UserID == "" {
		return platform.Result{}
	}
	profile := platform.Result{}
	if registered, ok := ctx.Value(popupAccountContextKey{}).(string); ok && registered != "" {
		profile["registered_at"] = registered
	}
	categories := map[string]bool{}
	products := map[string]platform.Result{}
	interests := map[string]bool{}
	addCategory := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" && len(value) <= 80 && len(categories) < 100 {
			categories[value] = true
		}
	}
	for _, fragment := range fragments {
		if fragment["error"] != nil || fragment["data"] == nil {
			continue
		}
		service, action := platform.Text(fragment, "service"), platform.Text(fragment, "action")
		data := platform.Map(fragment["data"])
		if fragment["auth"] == true {
			if service == constants.ServiceMember && action == constants.ActionGet {
				// These fields are produced by member/get, never accepted in HTTP data.
				if value, ok := data["growth"]; ok {
					profile["growth"] = value
				}
				if value := platform.Text(data, "level"); value != "" {
					profile["level"] = value
				}
				prefs := platform.Map(data["preferences"])
				for _, field := range []string{"categories", "interest_categories"} {
					for _, value := range popupStringList(prefs[field]) {
						addCategory(value)
					}
				}
			}
			if service == constants.ServicePoints && action == constants.ActionGet {
				if value, ok := data["available"]; ok {
					profile["points"] = value
				}
			}
			if service == constants.ServiceEngagement && action == constants.ActionList {
				command := platform.Map(fragment["command"])
				kind := platform.Text(platform.Map(command["data"]), "kind")
				if kind == constants.EngagementKindHistory || kind == constants.EngagementKindFavorite {
					for _, row := range resultRows(data) {
						if platform.Text(row, "user_id") == principal.UserID {
							interests[platform.Text(row, "sku_id")] = true
						}
					}
				}
			}
		}
		if service == constants.ServiceCatalog {
			if action == constants.ActionGet {
				products[platform.Text(data, "id")] = data
			}
			if action == constants.ActionList {
				for _, row := range resultRows(data) {
					products[platform.Text(row, "id")] = row
				}
			}
		}
	}
	for id := range interests {
		product := products[id]
		if product == nil {
			continue
		}
		addCategory(platform.Text(product, "category"))
		attributes := platform.Map(product["attributes"])
		for _, field := range []string{"category", "category_id", "category_name"} {
			addCategory(platform.Text(attributes, field))
		}
	}
	if len(categories) > 0 {
		values := make([]string, 0, len(categories))
		for value := range categories {
			values = append(values, value)
		}
		sort.Strings(values)
		profile["interest_categories"] = values
	}
	return profile
}
func popupStringList(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		out := []string{}
		for _, v := range values {
			if text, ok := v.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

// Popup qualification needs product categories, not stock or full page hydration.
// Resolve a bounded set in one catalog batch instead of issuing per-SKU calls.
func (g *gateway) popupInterestProducts(
	ctx context.Context,
	fragments []platform.Result,
	principal platform.Command,
) []platform.Result {
	plan := []readRequest{}
	seen := map[string]bool{}
	for _, fragment := range fragments {
		if platform.Text(fragment, "service") != constants.ServiceEngagement || fragment["error"] != nil ||
			fragment["auth"] != true {
			continue
		}
		for _, row := range resultRows(platform.Map(fragment["data"])) {
			id := platform.Text(row, "sku_id")
			if platform.Text(row, "user_id") != principal.UserID || id == "" || len(id) > 150 ||
				strings.TrimSpace(id) != id ||
				seen[id] ||
				len(plan) >= 50 {
				continue
			}
			seen[id] = true
			plan = append(
				plan,
				readRequest{
					Key:     "popup-interest-" + id,
					Service: constants.ServiceCatalog,
					Action:  constants.ActionGet,
					Command: readCommand{ID: id, Data: map[string]any{}},
				},
			)
		}
	}
	return append(fragments, g.runReads(ctx, plan, principal)...)
}
