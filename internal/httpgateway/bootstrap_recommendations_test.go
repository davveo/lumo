package httpgateway

import (
	"context"
	"testing"

	"github.com/davveo/lumo/internal/platform"
)

func TestBootstrapRanksVerifiedSignalsOnceAndPrimesNewRecommendedStock(t *testing.T) {
	caller := &aggregationCaller{
		handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
			if service == "catalog" && c.Action == "list" {
				return platform.Result{"items": []platform.Result{{"id": "base", "name": "Base"}}}, nil
			}
			if service == "engagement" {
				return platform.Result{
					"items": []platform.Result{
						{"user_id": c.UserID, "sku_id": "base", "kind": platform.Text(c.Data, "kind")},
					},
				}, nil
			}
			if service == "order" {
				return platform.Result{
					"items": []platform.Result{
						{"id": "own", "user_id": "buyer"},
						{"id": "other", "user_id": "someone-else"},
					},
				}, nil
			}
			if service == "inventory" && c.Action == "batch_get" {
				var input struct {
					IDs []string `json:"ids"`
				}
				_ = platform.Decode(c.Data, &input)
				rows := []platform.Result{}
				for _, id := range input.IDs {
					if id == "outside" {
						t.Error("ranker's fresh default stock was requested again")
					}
					rows = append(rows, platform.Result{"sku_id": id, "available": 5})
				}
				return platform.Result{"items": rows}, nil
			}
			if service == "recommendation" {
				if c.Action != "rank_bootstrap" || c.Role != "system" || c.UserID != "buyer" {
					t.Errorf("untrusted recommendation invocation: %#v", c)
				}
				if len(resultRows(platform.Result{"items": c.Data["catalog_items"]})) != 1 ||
					len(resultRows(platform.Result{"items": c.Data["inventory_items"]})) != 1 {
					t.Error("ranker did not receive already-loaded public data")
				}
				signals := platform.Map(c.Data["signals"])
				orders := resultRows(platform.Result{"items": signals["orders"]})
				if len(orders) != 1 || orders[0]["id"] != "own" {
					t.Errorf("ranking received another user's orders: %v", orders)
				}
				for _, kind := range []string{"favorite", "history", "compare", "search"} {
					if len(resultRows(platform.Result{"items": signals[kind]})) != 1 {
						t.Errorf("ranking did not reuse %s", kind)
					}
				}
				stock := []platform.Result{{"sku_id": "outside", "available": 7}}
				return platform.Result{
					"items": []platform.Result{
						{
							"id":       "outside",
							"name":     "Recommended",
							"variants": []platform.Result{{"sku_id": "outside-blue"}},
						},
					},
					"personalized":    true,
					"inventory_items": stock,
					"stock_snapshot":  stock,
				}, nil
			}
			return platform.Result{"items": []any{}}, nil
		},
	}
	code, body := batchHTTP(t, caller, "bootstrap", "admin", nil)
	if code != 200 {
		t.Fatal(code, body)
	}
	fragments := resultRows(platform.Result{"items": platform.Map(body["data"])["results"]})
	stockIDs, found := map[string]bool{}, false
	for _, fragment := range fragments {
		if fragment["service"] == "recommendation" {
			found = true
			data := platform.Map(fragment["data"])
			if fragment["auth"] != true || data["personalized"] != true || data["stock_snapshot"] != nil {
				t.Fatalf("wrong public ranking response: %v", fragment)
			}
		}
		if fragment["service"] == "inventory" {
			stockIDs[platform.Text(platform.Map(fragment["data"]), "sku_id")] = true
		}
	}
	if !found || !stockIDs["outside"] || !stockIDs["outside-blue"] {
		t.Fatalf("recommended item missing from bootstrap inventory primer: %v", fragments)
	}
	if caller.count("recommendation", "rank_bootstrap") != 1 || caller.count("order", "list") != 1 ||
		caller.count("engagement", "list") != 4 ||
		caller.count("inventory", "batch_get") != 2 {
		t.Fatalf("duplicate bootstrap inputs: %v", caller.calls)
	}
}
