package httpgateway

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func recentFragment(service, action, id, kind string, auth bool, data platform.Result) platform.Result {
	command := readCommand{ID: id, Data: map[string]any{}}
	if kind != "" {
		command.Data["kind"] = kind
	}
	return readResult(readRequest{Key: service + "-" + action + "-" + id + kind, Service: service, Action: action, Command: command, auth: auth}, data, nil)
}

func recentGets(fragments []platform.Result, service string) map[string]platform.Result {
	result := map[string]platform.Result{}
	for _, fragment := range fragments {
		if fragment["service"] == service && fragment["action"] == "get" {
			result[platform.Text(platform.Map(fragment["command"]), "id")] = platform.Map(fragment["data"])
		}
	}
	return result
}

func TestBootstrapRecentGuestCannotAcquireAnAccountRail(t *testing.T) {
	caller := &aggregationCaller{}
	fragments := []platform.Result{recentFragment("engagement", "list", "", "history", true, platform.Result{"items": []platform.Result{{"user_id": "someone", "sku_id": "private-interest"}}})}
	got := (&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{})
	if len(got) != len(fragments) || len(caller.calls) != 0 {
		t.Fatal("guest fetched a profile-derived product", got, caller.calls)
	}
}

func TestBootstrapRecentUsesOwnedSignalsAndSixKnownProductsWithoutRPC(t *testing.T) {
	history, products := []platform.Result{}, []platform.Result{}
	fragments := []platform.Result{}
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("mine-%d", i)
		history = append(history, platform.Result{"user_id": "buyer", "sku_id": id})
		products = append(products, platform.Result{"id": id, "name": id, "price_cents": 100})
		fragments = append(fragments, recentFragment("inventory", "get", id, "", false, platform.Result{"sku_id": id, "available": 3}))
	}
	history = append([]platform.Result{{"user_id": "foreign", "sku_id": "foreign-interest"}, {"sku_id": "missing-owner"}}, history...)
	history = append(history, platform.Result{"user_id": "buyer", "sku_id": "mine-0"}, platform.Result{"user_id": "buyer", "sku_id": " "}, platform.Result{"user_id": "buyer", "sku_id": strings.Repeat("x", 151)})
	fragments = append(fragments,
		recentFragment("catalog", "list", "", "", false, platform.Result{"items": products}),
		recentFragment("engagement", "list", "", "history", true, platform.Result{"items": history}),
		recentFragment("engagement", "list", "", "favorite", true, platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "extra-favorite"}}}),
		recentFragment("engagement", "list", "", "history", false, platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "unverified-interest"}}}),
	)
	caller := &aggregationCaller{}
	got := (&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{UserID: "buyer", Role: "admin"})
	gets := recentGets(got, "catalog")
	if len(gets) != bootstrapRecentLimit || len(caller.calls) != 0 {
		t.Fatal("known metadata was requested again or selection escaped its bound", gets, caller.calls)
	}
	for i := 0; i < bootstrapRecentLimit; i++ {
		if gets[fmt.Sprintf("mine-%d", i)]["name"] == nil {
			t.Fatal("recent order was not retained", gets)
		}
	}
	for _, fragment := range got[len(fragments):] {
		if fragment["auth"] != false || fragment["service"] != "catalog" {
			t.Fatal("public metadata primer gained account privileges or duplicate stock", fragment)
		}
	}
	if len(got) != len(fragments)+bootstrapRecentLimit {
		t.Fatal("existing stock fragments were duplicated", got)
	}
}

func TestBootstrapRecentFillsHistoryWithFavoritesAndKeepsUsersSeparate(t *testing.T) {
	products := []platform.Result{{"id": "history", "name": "History"}, {"id": "favorite", "name": "Favorite"}, {"id": "other", "name": "Other"}}
	fragments := []platform.Result{
		recentFragment("catalog", "list", "", "", false, platform.Result{"items": products}),
		recentFragment("engagement", "list", "", "history", true, platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "history"}, {"user_id": "other", "sku_id": "other"}}}),
		recentFragment("engagement", "list", "", "favorite", true, platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "history"}, {"user_id": "buyer", "sku_id": "favorite"}}}),
	}
	for _, product := range products {
		id := platform.Text(product, "id")
		fragments = append(fragments, recentFragment("inventory", "get", id, "", false, platform.Result{"sku_id": id, "available": 1}))
	}
	caller := &aggregationCaller{}
	for _, tc := range []struct {
		owner string
		ids   []string
	}{{"buyer", []string{"history", "favorite"}}, {"other", []string{"other"}}} {
		got := (&gateway{rpc: caller}).bootstrapRecent(context.Background(), append([]platform.Result(nil), fragments...), platform.Command{UserID: tc.owner})
		gets := recentGets(got, "catalog")
		if len(gets) != len(tc.ids) {
			t.Fatal("another account's interest was primed", tc.owner, gets)
		}
		for _, id := range tc.ids {
			if gets[id]["id"] != id {
				t.Fatal("owned interest missing", tc.owner, id)
			}
		}
	}
	if len(caller.calls) != 0 {
		t.Fatal("snapshot reuse made an unnecessary domain read", caller.calls)
	}
}

func TestBootstrapRecentResolvesRealVariantsWithTwoBulkMetadataReadsAndOneStockRead(t *testing.T) {
	caller := &aggregationCaller{handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
		if c.Action != "batch_get" || c.UserID != "" || c.Role != "" {
			t.Errorf("recent metadata is not a public bulk read: %s %#v", service, c)
		}
		var input struct {
			IDs []string `json:"ids"`
		}
		_ = platform.Decode(c.Data, &input)
		rows := []platform.Result{}
		for _, id := range input.IDs {
			if service == "inventory" {
				if id == "variant-blue" {
					t.Error("rail requested a variant stock instead of the canonical default")
				}
				rows = append(rows, platform.Result{"sku_id": id, "available": 7})
				continue
			}
			row := platform.Result{"id": id, "name": id, "price_cents": 100}
			if id == "variant-blue" {
				row["product_id"], row["color"], row["catalog_visible"] = "parent", "blue", false
			}
			if id == "parent" {
				row["variants"] = []platform.Result{{"sku_id": "parent"}, {"sku_id": "variant-blue"}}
			}
			rows = append(rows, row)
		}
		return platform.Result{"items": rows}, nil
	}}
	fragments := []platform.Result{recentFragment("engagement", "list", "", "history", true, platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "variant-blue"}, {"user_id": "buyer", "sku_id": "plain"}}})}
	got := (&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{UserID: "buyer", Role: "admin"})
	if caller.count("catalog", "batch_get") != 2 || caller.count("inventory", "batch_get") != 1 || caller.count("catalog", "get") != 0 || caller.count("inventory", "get") != 0 || caller.count("engagement", "list") != 0 {
		t.Fatal("recent goods made per-SKU or repeated behavior reads", caller.calls)
	}
	gets := recentGets(got, "catalog")
	if gets["variant-blue"]["id"] != "variant-blue" || gets["variant-blue"]["color"] != "blue" || gets["parent"]["id"] != "parent" || len(resultRows(platform.Result{"items": gets["parent"]["variants"]})) != 2 {
		t.Fatal("variant snapshot was replaced by incomplete parent metadata", gets)
	}
	if stocks := recentGets(got, "inventory"); len(stocks) != 2 || stocks["parent"]["available"] == nil || stocks["plain"]["available"] == nil {
		t.Fatal("canonical default stock was not primed", stocks)
	}
	keys := map[string]bool{}
	for _, fragment := range got {
		key := platform.Text(fragment, "key")
		if keys[key] {
			t.Fatal("duplicate bootstrap fragment key", key)
		}
		keys[key] = true
	}
}

func TestBootstrapRecentReusesRecommendedParentAndStockWithoutImpersonatingVariant(t *testing.T) {
	caller := &aggregationCaller{handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
		var input struct {
			IDs []string `json:"ids"`
		}
		_ = platform.Decode(c.Data, &input)
		if service != "catalog" || c.Action != "batch_get" || !reflect.DeepEqual(input.IDs, []string{"variant"}) {
			t.Fatalf("unexpected read with all parent data already loaded: %s %#v", service, c)
		}
		return platform.Result{"items": []platform.Result{{"id": "variant", "product_id": "parent", "name": "Variant", "color": "blue", "catalog_visible": false}}}, nil
	}}
	fragments := []platform.Result{
		recentFragment("engagement", "list", "", "history", true, platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "variant"}}}),
		recentFragment("recommendation", "list", "", "", true, platform.Result{"items": []platform.Result{{"id": "parent", "name": "Parent", "recommendation_reason": "personal explanation", "variants": []platform.Result{{"sku_id": "parent"}, {"sku_id": "variant"}}}}, "inventory_items": []platform.Result{{"sku_id": "parent", "available": 9}}}),
	}
	got := (&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{UserID: "buyer"})
	if len(caller.calls) != 1 {
		t.Fatal("already-read default metadata or stock was requested again", caller.calls)
	}
	gets := recentGets(got, "catalog")
	if gets["variant"]["id"] != "variant" || gets["parent"]["id"] != "parent" || gets["parent"]["recommendation_reason"] != nil {
		t.Fatal("catalog primer changed its resource or leaked ranking annotations", gets)
	}
	if len(recentGets(got, "inventory")) != 1 {
		t.Fatal("fresh recommendation inventory was not reused")
	}
}

func TestBootstrapRecentMetadataFailureIsIsolatedAndNotInvented(t *testing.T) {
	caller := &aggregationCaller{handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
		if service != "catalog" || c.Action != "batch_get" {
			t.Fatal("missing product still caused a stock read", service, c)
		}
		return nil, status.Error(codes.Unavailable, "catalog offline")
	}}
	fragments := []platform.Result{recentFragment("engagement", "list", "", "history", true, platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "missing"}}})}
	got := (&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{UserID: "buyer"})
	if len(got) != 2 || got[1]["data"] != nil || platform.Int(platform.Map(got[1]["error"]), "status") != 503 || len(caller.calls) != 1 {
		t.Fatal("upstream failure was hidden or multiplied", got, caller.calls)
	}
}
