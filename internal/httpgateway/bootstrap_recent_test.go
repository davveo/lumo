package httpgateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	return readResult(
		readRequest{
			Key:     service + "-" + action + "-" + id + kind,
			Service: service,
			Action:  action,
			Command: command,
			auth:    auth,
		},
		data,
		nil,
	)
}

func recentProduct(id string) platform.Result {
	return platform.Result{
		"id":              id,
		"name":            id,
		"price_cents":     100,
		"status":          "ACTIVE",
		"active":          true,
		"catalog_visible": true,
		"variants":        []platform.Result{{"sku_id": id}, {"sku_id": id + "-blue"}},
	}
}

func recentPublic(products ...platform.Result) []platform.Result {
	fragments := []platform.Result{
		readResult(
			readRequest{
				Key:     "catalog",
				Service: "catalog",
				Action:  "list",
				Command: readCommand{Data: map[string]any{"limit": 100}},
			},
			platform.Result{"items": products, "total": len(products)},
			nil,
		),
	}
	for _, product := range products {
		id := platform.Text(product, "id")
		fragments = append(
			fragments,
			recentFragment("inventory", "get", id, "", false, platform.Result{"sku_id": id, "available": 5}),
		)
	}
	return fragments
}

func recentData(t *testing.T, fragments []platform.Result, auth bool) platform.Result {
	t.Helper()
	last := fragments[len(fragments)-1]
	command := platform.Map(last["command"])
	if last["key"] != "bootstrap-recent" || last["service"] != "storefront" || last["action"] != "recent" ||
		last["auth"] != auth ||
		len(platform.Map(command["data"])) != 0 ||
		last["error"] != nil {
		t.Fatalf("invalid recent primer: %#v", last)
	}
	data := platform.Map(last["data"])
	items, inventory := resultRows(data), resultRows(platform.Result{"items": data["inventory_items"]})
	if len(items) != len(inventory) || platform.Int(data, "limit") != 6 ||
		int(platform.Int(data, "count")) != len(items) {
		t.Fatal("recent is not a complete bounded page", data)
	}
	seen := map[string]bool{}
	for i, item := range items {
		id := platform.Text(item, "id")
		if seen[id] || recentParent(item) != id || !recentVisible(item) || inventory[i]["sku_id"] != id ||
			platform.Int(inventory[i], "available") < 1 {
			t.Fatal("rail contains duplicate/hidden/stockless SKU rather than a real parent", data)
		}
		seen[id] = true
	}
	return data
}

func recentIDs(data platform.Result) []string {
	ids := []string{}
	for _, item := range resultRows(data) {
		ids = append(ids, platform.Text(item, "id"))
	}
	return ids
}

func TestBootstrapRecentUsesOwnedHistoryDistinctParentsThenRankedAndFeaturedWithoutRPC(t *testing.T) {
	products := []platform.Result{}
	for i := 0; i < 9; i++ {
		products = append(products, recentProduct(fmt.Sprintf("p%d", i)))
	}
	fragments := recentPublic(products...)
	fragments = append(
		fragments,
		recentFragment(
			"engagement",
			"list",
			"",
			"history",
			true,
			platform.Result{
				"items": []platform.Result{
					{"user_id": "other", "sku_id": "p8"},
					{"sku_id": "p7"},
					{"user_id": "buyer", "sku_id": "p0-blue"},
					{"user_id": "buyer", "sku_id": "p0"},
					{"user_id": "buyer", "sku_id": "p1-blue"},
				},
			},
		),
		recentFragment(
			"engagement",
			"list",
			"",
			"favorite",
			true,
			platform.Result{
				"items": []platform.Result{{"user_id": "buyer", "sku_id": "p0"}, {"user_id": "buyer", "sku_id": "p2"}},
			},
		),
		recentFragment(
			"engagement",
			"list",
			"",
			"history",
			false,
			platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "p7"}}},
		),
	)
	four, three := recentProduct("p4"), recentProduct("p3")
	four["recommendation_reason"] = "根据你的偏好"
	fragments = append(
		fragments,
		recentFragment(
			"recommendation",
			"list",
			"",
			"",
			true,
			platform.Result{"items": []platform.Result{four, three}, "personalized": true},
		),
	)
	caller := &aggregationCaller{}
	got := (&gateway{rpc: caller}).bootstrapRecent(
		context.Background(),
		fragments,
		platform.Command{UserID: "buyer", Role: "admin"},
	)
	data := recentData(t, got, true)
	if want := []string{"p0", "p1", "p2", "p4", "p3", "p5"}; !reflect.DeepEqual(recentIDs(data), want) ||
		data["personalized"] != true ||
		len(caller.calls) != 0 {
		t.Fatal("owned signals/ranking were not reused as one six-parent page", data, caller.calls)
	}
	items := resultRows(data)
	if items[0]["recent_source"] != "history" || items[2]["recent_source"] != "favorite" ||
		items[3]["recommendation_reason"] != "根据你的偏好" ||
		items[5]["recent_source"] != "featured" ||
		len(resultRows(platform.Result{"items": items[0]["variants"]})) != 2 {
		t.Fatal("real metadata and source annotation were lost", data)
	}
	if products[0]["recent_source"] != nil {
		t.Fatal("recent annotations mutated the public catalog snapshot")
	}
}

func TestBootstrapRecentGuestHasSixRealGoodsAndNeverAcquiresAccountSignals(t *testing.T) {
	products := []platform.Result{}
	for i := 0; i < 8; i++ {
		products = append(products, recentProduct(fmt.Sprintf("p%d", i)))
	}
	fragments := recentPublic(products...)
	fragments = append(
		fragments,
		recentFragment(
			"engagement",
			"list",
			"",
			"history",
			true,
			platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "p7"}}},
		),
	)
	// A private recommendation fragment must not seed the guest page either.
	fragments = append(
		fragments,
		recentFragment(
			"recommendation",
			"list",
			"",
			"",
			true,
			platform.Result{"items": []platform.Result{products[7]}, "personalized": true},
		),
	)
	caller := &aggregationCaller{}
	data := recentData(
		t,
		(&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{}),
		false,
	)
	if !reflect.DeepEqual(recentIDs(data), []string{"p0", "p1", "p2", "p3", "p4", "p5"}) ||
		data["personalized"] != false ||
		data["degraded"] != false ||
		len(caller.calls) != 0 {
		t.Fatal("guest did not receive an isolated real featured page", data, caller.calls)
	}
}

func TestBootstrapRecentKeepsAccountsSeparateAndReturnsHonestSmallCatalog(t *testing.T) {
	fragments := recentPublic(recentProduct("a"), recentProduct("b"), recentProduct("c"))
	fragments = append(
		fragments,
		recentFragment(
			"engagement",
			"list",
			"",
			"history",
			true,
			platform.Result{
				"items": []platform.Result{{"user_id": "alice", "sku_id": "c"}, {"user_id": "bob", "sku_id": "b"}},
			},
		),
	)
	for _, owner := range []string{"alice", "bob", "carol"} {
		caller := &aggregationCaller{}
		data := recentData(
			t,
			(&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{UserID: owner}),
			true,
		)
		want := map[string][]string{"alice": {"c", "a", "b"}, "bob": {"b", "a", "c"}, "carol": {"a", "b", "c"}}
		if !reflect.DeepEqual(recentIDs(data), want[owner]) || len(caller.calls) != 0 ||
			data["personalized"] != (owner != "carol") {
			t.Fatal("account preference leaked or catalog shortage was fabricated", owner, data)
		}
	}
}

func TestBootstrapRecentSkipsUnavailableAndDuplicateSeedsAndBulkResolvesRealParents(t *testing.T) {
	products := []platform.Result{}
	for i := 0; i < 6; i++ {
		products = append(products, recentProduct(fmt.Sprintf("featured-%d", i)))
	}
	fragments := recentPublic(products...)
	fragments = append(
		fragments,
		recentFragment("engagement", "list", "", "history", true, platform.Result{"items": []platform.Result{
			{
				"user_id": "buyer",
				"sku_id":  "missing",
			},
			{"user_id": "buyer", "sku_id": "disabled"},
			{"user_id": "buyer", "sku_id": "variant"},
			{"user_id": "buyer", "sku_id": "variant"},
			{"user_id": "buyer", "sku_id": " "},
			{"user_id": "buyer", "sku_id": strings.Repeat("x", 151)},
		}}),
	)
	caller := &aggregationCaller{
		handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
			if c.Action != "batch_get" || c.UserID != "" || c.Role != "" {
				t.Fatalf("recent moved to per-item or privileged metadata reads: %s %#v", service, c)
			}
			var input struct {
				IDs []string `json:"ids"`
			}
			_ = platform.Decode(c.Data, &input)
			rows := []platform.Result{}
			for _, id := range input.IDs {
				if service == "inventory" {
					if id != "parent" {
						t.Errorf("missing/disabled/variant stock requested: %q", id)
					}
					rows = append(rows, platform.Result{"sku_id": id, "available": 7})
					continue
				}
				switch id {
				case "variant":
					rows = append(
						rows,
						platform.Result{"id": id, "product_id": "parent", "catalog_visible": false, "color": "blue"},
					)
				case "parent":
					rows = append(rows, recentProduct(id))
				case "disabled":
					row := recentProduct(id)
					row["active"] = false
					rows = append(rows, row)
				case "missing": // no invented metadata for a deleted SKU
				default:
					t.Errorf("unexpected metadata ID: %q", id)
				}
			}
			return platform.Result{"items": rows}, nil
		},
	}
	got := (&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{UserID: "buyer"})
	data := recentData(t, got, true)
	if want := []string{
		"parent",
		"featured-0",
		"featured-1",
		"featured-2",
		"featured-3",
		"featured-4",
	}; !reflect.DeepEqual(
		recentIDs(data),
		want,
	) ||
		caller.count("catalog", "batch_get") != 2 ||
		caller.count("inventory", "batch_get") != 1 ||
		caller.count("engagement", "list") != 0 {
		t.Fatal("parent resolution was not bounded or misses were counted as rail entries", data, caller.calls)
	}
	variant := platform.Result{}
	for _, fragment := range got {
		if fragment["service"] == "catalog" && platform.Text(platform.Map(fragment["command"]), "id") == "variant" {
			variant = platform.Map(fragment["data"])
		}
	}
	if variant["id"] != "variant" || variant["color"] != "blue" {
		t.Fatal("variant primer was impersonated by its parent", variant)
	}
}

func TestBootstrapRecentFillsPastSixDuplicateVariantsAndSkipsOutOfStock(t *testing.T) {
	products := []platform.Result{}
	for i := 0; i < 8; i++ {
		products = append(products, recentProduct(fmt.Sprintf("p%d", i)))
	}
	products[7]["active"] = false
	fragments := recentPublic(products...)
	history := []platform.Result{}
	for i := 0; i < 12; i++ {
		history = append(history, platform.Result{"user_id": "buyer", "sku_id": "p0-blue"})
	}
	history = append(
		history,
		platform.Result{"user_id": "buyer", "sku_id": "p6-blue"},
		platform.Result{"user_id": "buyer", "sku_id": "p7"},
	)
	fragments = append(
		fragments,
		recentFragment("engagement", "list", "", "history", true, platform.Result{"items": history}),
	)
	fragments = append(
		fragments,
		recentFragment("inventory", "get", "p0", "", false, platform.Result{"sku_id": "p0", "available": 0}),
	)
	caller := &aggregationCaller{}
	data := recentData(
		t,
		(&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{UserID: "buyer"}),
		true,
	)
	if !reflect.DeepEqual(recentIDs(data), []string{"p6", "p1", "p2", "p3", "p4", "p5"}) || len(caller.calls) != 0 {
		t.Fatal("six raw SKUs were mistaken for six displayable parents", data, caller.calls)
	}
}

func TestBootstrapRecentScansNextRealCatalogPageOnlyWhenFirstPageCannotFill(t *testing.T) {
	products := []platform.Result{}
	for i := 0; i < 100; i++ {
		products = append(products, recentProduct(fmt.Sprintf("empty-%d", i)))
	}
	fragments := recentPublic(products...)
	fragments[0]["data"].(platform.Result)["total"] = 108
	for _, fragment := range fragments[1:] {
		fragment["data"].(platform.Result)["available"] = 0
	}
	caller := &aggregationCaller{
		handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
			if c.UserID != "" || c.Role != "" {
				t.Fatal("fallback acquired account permissions", c)
			}
			rows := []platform.Result{}
			if service == "catalog" && c.Action == "list" {
				if platform.Int(c.Data, "offset") != 100 || platform.Int(c.Data, "limit") != 100 {
					t.Fatal("invalid fallback page", c)
				}
				for i := 0; i < 8; i++ {
					rows = append(rows, recentProduct(fmt.Sprintf("available-%d", i)))
				}
				return platform.Result{"items": rows, "total": 108}, nil
			}
			if service != "inventory" || c.Action != "batch_get" {
				t.Fatal("fallback used per-SKU reads", service, c)
			}
			var input struct {
				IDs []string `json:"ids"`
			}
			_ = platform.Decode(c.Data, &input)
			for _, id := range input.IDs {
				rows = append(rows, platform.Result{"sku_id": id, "available": 2})
			}
			return platform.Result{"items": rows}, nil
		},
	}
	data := recentData(
		t,
		(&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{}),
		false,
	)
	if len(resultRows(data)) != 6 || recentIDs(data)[0] != "available-0" || len(caller.calls) != 2 {
		t.Fatal("first 100 unavailable goods were mistaken for a global shortage", data, caller.calls)
	}
}

func TestBootstrapRecentDependencyFailureKeepsKnownGoodsAndNeverInventsStock(t *testing.T) {
	fragments := recentPublic(recentProduct("real"))
	fragments = append(
		fragments,
		recentFragment(
			"engagement",
			"list",
			"",
			"history",
			true,
			platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "missing"}}},
		),
	)
	fragments = append(
		fragments,
		readResult(
			readRequest{Service: "recommendation", Action: "list", auth: true},
			nil,
			status.Error(codes.Unavailable, "ranking offline"),
		),
	)
	caller := &aggregationCaller{
		handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
			if service != "catalog" || c.Action != "batch_get" {
				t.Fatal("missing metadata still caused a stock read", service, c)
			}
			return nil, status.Error(codes.Unavailable, "catalog offline")
		},
	}
	data := recentData(
		t,
		(&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{UserID: "buyer"}),
		true,
	)
	if !reflect.DeepEqual(recentIDs(data), []string{"real"}) || data["degraded"] != true || len(caller.calls) != 1 {
		t.Fatal("degraded state swallowed or missing item invented", data, caller.calls)
	}
}

func TestBootstrapRecentCriticalCatalogFailureIsNotSuccessfulEmpty(t *testing.T) {
	fragments := []platform.Result{
		readResult(
			readRequest{Service: "catalog", Action: "list"},
			nil,
			status.Error(codes.Unavailable, "catalog offline"),
		),
	}
	caller := &aggregationCaller{}
	got := (&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{})
	last := got[len(got)-1]
	if last["data"] != nil || platform.Int(platform.Map(last["error"]), "status") != 503 || len(caller.calls) != 0 {
		t.Fatal("failed catalog became an empty successful rail", got, caller.calls)
	}
}

func TestStorefrontRecentReusesOneVerifiedSignalAndOrderSnapshotForRanking(t *testing.T) {
	products := []platform.Result{}
	for i := 0; i < 8; i++ {
		products = append(products, recentProduct(fmt.Sprintf("p%d", i)))
	}
	caller := &aggregationCaller{
		handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
			switch service {
			case "catalog":
				if c.Action != "list" || c.UserID != "" || c.Role != "" {
					t.Fatal("unexpected catalog read", c)
				}
				return platform.Result{"items": products, "total": len(products)}, nil
			case "engagement":
				if c.UserID != "buyer" || c.Role != "user" {
					t.Fatal("private read lost verified principal", c)
				}
				return platform.Result{
					"items": []platform.Result{
						{"user_id": c.UserID, "sku_id": "p7", "kind": platform.Text(c.Data, "kind"), "query": "coffee"},
					},
				}, nil
			case "order":
				return platform.Result{
					"items": []platform.Result{
						{"user_id": "buyer", "status": "PAID", "items": []platform.Result{{"sku_id": "p6"}}},
						{"user_id": "victim", "status": "PAID"},
					},
				}, nil
			case "inventory":
				if c.Action != "batch_get" || c.UserID != "" {
					t.Fatal("unexpected inventory read", c)
				}
				var input struct {
					IDs []string `json:"ids"`
				}
				_ = platform.Decode(c.Data, &input)
				rows := []platform.Result{}
				for _, id := range input.IDs {
					rows = append(rows, platform.Result{"sku_id": id, "available": 4})
				}
				return platform.Result{"items": rows}, nil
			case "recommendation":
				if c.Action != "rank_bootstrap" || c.UserID != "buyer" || c.Role != "system" {
					t.Fatal("ranker received browser profile claims", c)
				}
				signals := platform.Map(c.Data["signals"])
				for _, kind := range []string{"history", "favorite", "compare", "search", "orders"} {
					if len(resultRows(platform.Result{"items": signals[kind]})) != 1 {
						t.Fatalf("%s snapshot lost or foreign order included: %v", kind, signals)
					}
				}
				return platform.Result{
					"items": []platform.Result{
						products[5],
						products[4],
						products[3],
						products[2],
						products[1],
						products[0],
					},
					"personalized": true,
				}, nil
			}
			t.Fatal("recent loaded unrelated page state", service, c)
			return nil, nil
		},
	}
	code, response := batchHTTP(t, caller, "recent", "buyer", nil)
	if code != 200 {
		t.Fatal(code, response)
	}
	data := platform.Map(response["data"])
	if !reflect.DeepEqual(recentIDs(data), []string{"p7", "p5", "p4", "p3", "p2", "p1"}) ||
		data["personalized"] != true ||
		caller.count("engagement", "list") != 4 ||
		caller.count("order", "list") != 1 ||
		caller.count("catalog", "list") != 1 ||
		caller.count("inventory", "batch_get") != 1 ||
		caller.count("recommendation", "rank_bootstrap") != 1 {
		t.Fatal("refresh repeated account/ranking inputs or omitted the complete page", data, caller.calls)
	}
}

func TestStorefrontRecentGuestIsPublicButParametersGETAndNestedReadsAreRejected(t *testing.T) {
	caller := &aggregationCaller{}
	code, response := batchHTTP(t, caller, "recent", "", nil)
	if code != 200 || platform.Map(response["data"])["personalized"] != false ||
		caller.count("engagement", "list") != 0 ||
		caller.count("order", "list") != 0 {
		t.Fatal("guest acquired account state", code, response, caller.calls)
	}
	for _, body := range []string{
		`{"id":"product"}`,
		`{"data":{"limit":6}}`,
		`{"data":{"user_id":"victim"}}`,
		`{"data":{"profile":{"favorite":["private"]}}}`,
	} {
		before := len(caller.calls)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/storefront/recent", strings.NewReader(body))
		w := httptest.NewRecorder()
		NewHandler(caller, false, t.TempDir()).ServeHTTP(w, r)
		if w.Code != 400 || len(caller.calls) != before {
			t.Fatal(
				"recent accepted client-controlled scope or made reads before preflight",
				body,
				w.Code,
				caller.calls[before:],
			)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/storefront/recent", nil)
	w := httptest.NewRecorder()
	NewHandler(caller, false, t.TempDir()).ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("unexpected GET exposure", w.Code)
	}
	code, _ = batchHTTP(t, caller, "batch", "buyer", []any{readItem("rail", "storefront", "recent", "")})
	if code != 403 {
		t.Fatal("recent was recursively exposed in a general batch", code)
	}
}

func TestBootstrapRecentInventoryOutageStopsUnproductiveCatalogScan(t *testing.T) {
	products := []platform.Result{}
	for i := 0; i < 100; i++ {
		products = append(products, recentProduct(fmt.Sprintf("p%d", i)))
	}
	fragments := recentPublic(products...)
	fragments[0]["data"].(platform.Result)["total"] = 100000
	// Only one previously confirmed item remains usable. A failed shared stock
	// dependency must not trigger one catalog + stock round trip per 100 goods.
	for i := 2; i < len(fragments); i++ {
		fragment := fragments[i]
		request := readRequest{
			Key:     platform.Text(fragment, "key"),
			Service: "inventory",
			Action:  "get",
			Command: readCommand{ID: platform.Text(platform.Map(fragment["command"]), "id"), Data: map[string]any{}},
		}
		fragments[i] = readResult(request, nil, status.Error(codes.Unavailable, "inventory offline"))
	}
	caller := &aggregationCaller{
		handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
			t.Fatal("outage multiplied into a full catalog scan", service, c)
			return nil, nil
		},
	}
	data := recentData(
		t,
		(&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{}),
		false,
	)
	if !reflect.DeepEqual(recentIDs(data), []string{"p0"}) || data["degraded"] != true || len(caller.calls) != 0 {
		t.Fatal("outage discarded usable real results or pretended a confirmed shortage", data, caller.calls)
	}
}

func TestBootstrapRecentInvalidMetadataNeverBecomesDisplayableGoods(t *testing.T) {
	valid := recentProduct("valid")
	missingName, zeroPrice, disabled, hidden, variant := recentProduct(
		"name",
	), recentProduct(
		"price",
	), recentProduct(
		"disabled",
	), recentProduct(
		"hidden",
	), recentProduct(
		"variant",
	)
	missingName["name"],
		zeroPrice["price_cents"],
		disabled["status"],
		hidden["catalog_visible"],
		variant["product_id"] = " ", 0, "DISABLED", false, "valid"
	fragments := recentPublic(missingName, zeroPrice, disabled, hidden, variant, valid)
	caller := &aggregationCaller{}
	data := recentData(
		t,
		(&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{}),
		false,
	)
	if !reflect.DeepEqual(recentIDs(data), []string{"valid"}) || len(caller.calls) != 0 {
		t.Fatal("missing/disabled/variant data counted toward the parent quota", data, caller.calls)
	}
}

func TestBootstrapRecentDistinctDisabledPageDoesNotStopLaterSellablePage(t *testing.T) {
	products := []platform.Result{}
	for i := 0; i < 100; i++ {
		products = append(products, recentProduct(fmt.Sprintf("first-%d", i)))
	}
	fragments := recentPublic(products...)
	fragments[0]["data"].(platform.Result)["total"] = 206
	for _, fragment := range fragments[1:] {
		fragment["data"].(platform.Result)["available"] = 0
	}
	caller := &aggregationCaller{
		handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
			rows := []platform.Result{}
			if service == "catalog" && c.Action == "list" {
				offset := platform.Int(c.Data, "offset")
				switch offset {
				case 100:
					for i := 0; i < 100; i++ {
						row := recentProduct(fmt.Sprintf("disabled-%d", i))
						row["catalog_visible"] = false
						rows = append(rows, row)
					}
				case 200:
					for i := 0; i < 6; i++ {
						rows = append(rows, recentProduct(fmt.Sprintf("available-%d", i)))
					}
				default:
					t.Fatal("unexpected page", c)
				}
				return platform.Result{"items": rows, "total": 206}, nil
			}
			if service != "inventory" || c.Action != "batch_get" {
				t.Fatal("fallback is not batched", service, c)
			}
			var input struct {
				IDs []string `json:"ids"`
			}
			_ = platform.Decode(c.Data, &input)
			for _, id := range input.IDs {
				if !strings.HasPrefix(id, "available-") {
					t.Fatal("hidden goods caused stock reads", id)
				}
				rows = append(rows, platform.Result{"sku_id": id, "available": 3})
			}
			return platform.Result{"items": rows}, nil
		},
	}
	data := recentData(
		t,
		(&gateway{rpc: caller}).bootstrapRecent(context.Background(), fragments, platform.Command{}),
		false,
	)
	if len(resultRows(data)) != 6 || data["degraded"] != false || caller.count("catalog", "list") != 2 ||
		caller.count("inventory", "batch_get") != 1 {
		t.Fatal("valid pagination was confused with repeated dependency data", data, caller.calls)
	}
}

func TestBootstrapRecentCancelledContextReturnsErrorInsteadOfEmptyOrPartialSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	caller := &aggregationCaller{}
	fragments := (&gateway{rpc: caller}).bootstrapRecent(ctx, recentPublic(recentProduct("real")), platform.Command{})
	last := fragments[len(fragments)-1]
	if last["data"] != nil || platform.Text(platform.Map(last["error"]), "code") != "Canceled" ||
		len(caller.calls) != 0 {
		t.Fatal("cancelled request produced a usable stale page", last, caller.calls)
	}
}
