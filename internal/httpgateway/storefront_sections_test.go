package httpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func sectionHTTP(
	t *testing.T,
	caller platform.Caller,
	section, token string,
	data map[string]any,
) (int, platform.Result) {
	t.Helper()
	if data == nil {
		data = map[string]any{}
	}
	payload, _ := json.Marshal(platform.Command{Data: data})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/storefront/"+section, bytes.NewReader(payload))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	NewHandler(caller, false, "").ServeHTTP(w, r)
	var body platform.Result
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return w.Code, body
}
func sectionRows(body platform.Result) []platform.Result {
	return resultRows(platform.Result{"items": platform.Map(body["data"])["results"]})
}
func TestStorefrontShellAndBootstrapOnlyReadPublicConfiguration(t *testing.T) {
	for _, section := range []string{"shell", "bootstrap"} {
		t.Run(section, func(t *testing.T) {
			c := &aggregationCaller{
				handle: func(_ context.Context, service string, request platform.Command) (platform.Result, error) {
					if service != "content" || request.Action != "get" || request.UserID != "" {
						t.Errorf("shell unexpected dependency: %s %+v", service, request)
					}
					return platform.Result{"id": request.ID, "kind": "storefront", "body": "{}"}, nil
				},
			}
			code, body := sectionHTTP(t, c, section, "buyer", nil)
			if code != 200 || len(sectionRows(body)) != 2 {
				t.Fatalf("status=%d body=%v", code, body)
			}
			for _, service := range []string{"member", "order", "address", "coupon", "inventory", "catalog", "helpdesk", "notification"} {
				if c.count(service, "list")+c.count(service, "get") != 0 {
					t.Fatalf("shell fetched %s", service)
				}
			}
			if strings.Contains(fmt.Sprint(body), "popup") {
				t.Fatal("optional popup leaked into shell")
			}
		})
	}
}
func TestStorefrontProductsBatchAllVariantStockWithoutPrivateDependencies(t *testing.T) {
	c := &aggregationCaller{
		handle: func(_ context.Context, service string, request platform.Command) (platform.Result, error) {
			if service == "catalog" {
				if request.Action != "list" || request.UserID != "" {
					t.Errorf("unexpected catalog request %+v", request)
				}
				return platform.Result{
					"items": []platform.Result{{"id": "parent", "variants": []platform.Result{{"sku_id": "option"}}}},
					"total": 1,
				}, nil
			}
			if service == "inventory" {
				if request.Action != "batch_get" || request.UserID != "" {
					t.Errorf("not a public stock batch %+v", request)
				}
				return platform.Result{
					"items": []platform.Result{
						{"sku_id": "parent", "available": 5},
						{"sku_id": "option", "available": 4},
					},
				}, nil
			}
			t.Errorf("unexpected dependency %s", service)
			return platform.Result{}, nil
		},
	}
	code, body := sectionHTTP(t, c, "products", "buyer", nil)
	if code != 200 || len(sectionRows(body)) != 2 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	if c.count("inventory", "batch_get") != 1 || c.count("inventory", "get") != 0 {
		t.Fatal("stock N+1 regression")
	}
}
func TestStorefrontActivitiesBoundOfferMetadataAndOnlyTwoCouponPreviews(t *testing.T) {
	c := &aggregationCaller{
		handle: func(_ context.Context, service string, request platform.Command) (platform.Result, error) {
			if service == "coupon" {
				if platform.Int(request.Data, "page_size") != 2 {
					t.Errorf("not preview: %+v", request.Data)
				}
				return platform.Result{"items": []any{}}, nil
			}
			if service == "flash" {
				rows := []platform.Result{}
				for i := 0; i < 601; i++ {
					rows = append(rows, platform.Result{"id": fmt.Sprint(i), "sku_id": fmt.Sprintf("sku-%03d", i)})
				}
				return platform.Result{"items": rows}, nil
			}
			if service == "catalog" || service == "inventory" {
				var ids []string
				raw, _ := json.Marshal(request.Data["ids"])
				_ = json.Unmarshal(raw, &ids)
				if request.Action != "batch_get" || len(ids) > 500 {
					t.Errorf("unbounded metadata %+v", request)
				}
				rows := []platform.Result{}
				for _, id := range ids {
					rows = append(rows, platform.Result{"id": id, "sku_id": id})
				}
				return platform.Result{"items": rows}, nil
			}
			return platform.Result{"items": []any{}}, nil
		},
	}
	code, body := sectionHTTP(t, c, "activities", "buyer", nil)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	if c.count("catalog", "batch_get") != 2 || c.count("inventory", "batch_get") != 2 {
		t.Fatal("metadata must split at 500 IDs")
	}
	for _, row := range sectionRows(body) {
		if row["service"] == "flash" && len(resultRows(platform.Map(row["data"]))) != 601 {
			t.Fatal("a preview must not masquerade as a full flash list")
		}
	}
}
func TestStorefrontAccountSummaryProjectsOnlyCountsAndPublicProfile(t *testing.T) {
	c := &aggregationCaller{
		handle: func(_ context.Context, service string, request platform.Command) (platform.Result, error) {
			if request.UserID != "buyer" {
				t.Errorf("missing owner %+v", request)
			}
			switch service {
			case "member":
				return platform.Result{
					"display_name": "Buyer",
					"level":        "GOLD",
					"preferences":  map[string]any{"phone": "private"},
				}, nil
			case "points":
				return nil, status.Error(codes.Unavailable, "points offline")
			case "coupon":
				return platform.Result{
					"items": []platform.Result{
						{
							"id":         "valid",
							"status":     "AVAILABLE",
							"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
						},
						{
							"id":         "expired",
							"status":     "AVAILABLE",
							"expires_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
						},
					},
				}, nil
			case "engagement":
				return platform.Result{"items": []platform.Result{{"sku_id": "a"}, {"sku_id": "b"}}}, nil
			case "cart":
				return platform.Result{"items": []platform.Result{{"quantity": 2}, {"quantity": 1}}}, nil
			case "notification":
				return platform.Result{"items": []platform.Result{{"status": "UNREAD"}, {"status": "READ"}}}, nil
			}
			t.Errorf("unrelated account dependency %s", service)
			return platform.Result{}, nil
		},
	}
	code, body := sectionHTTP(t, c, "account_summary", "buyer", nil)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	data := platform.Map(body["data"])
	summary := platform.Map(data["summary"])
	if platform.Int(summary, "cart_count") != 3 || platform.Int(summary, "favorite_count") != 2 ||
		platform.Int(summary, "coupon_count") != 1 ||
		platform.Int(summary, "unread_count") != 1 {
		t.Fatalf("wrong summary %+v", summary)
	}
	if summary["points"] != nil || platform.Map(data["errors"])["points"] == nil {
		t.Fatal("failed dependency invented zero")
	}
	for _, secret := range []string{"private", "preferences", "expires_at", "quantity"} {
		if strings.Contains(fmt.Sprint(data), secret) {
			t.Fatalf("summary leaks full detail %q", secret)
		}
	}
}
func TestStorefrontAccountSummaryUsesExactTotalsWithoutExpandingPrivateLists(t *testing.T) {
	c := &aggregationCaller{
		handle: func(_ context.Context, service string, request platform.Command) (platform.Result, error) {
			if request.UserID != "buyer" {
				t.Errorf("summary missing verified owner: %+v", request)
			}
			switch service {
			case "cart":
				return platform.Result{
					"items":          []platform.Result{{"quantity": 2}},
					"total":          151,
					"quantity_total": 302,
				}, nil
			case "engagement":
				if platform.Text(request.Data, "kind") != "favorite" || platform.Int(request.Data, "limit") != 100 {
					t.Errorf("unbounded favorite projection: %+v", request)
				}
				items := []platform.Result{}
				for i := 0; i < 100; i++ {
					items = append(items, platform.Result{"sku_id": fmt.Sprintf("owned-%d", i)})
				}
				return platform.Result{"items": items, "total": 151}, nil
			case "notification":
				if platform.Int(request.Data, "limit") != 100 {
					t.Errorf("unbounded notification projection: %+v", request)
				}
				// The newest page can be entirely read while older unread rows remain.
				return platform.Result{
					"items":        []platform.Result{{"status": "READ"}},
					"total":        300,
					"unread_total": 125,
				}, nil
			}
			return platform.Result{"items": []any{}}, nil
		},
	}
	code, body := sectionHTTP(t, c, "account_summary", "buyer", nil)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	summary := platform.Map(platform.Map(body["data"])["summary"])
	if platform.Int(summary, "favorite_count") != 151 || platform.Int(summary, "unread_count") != 125 ||
		platform.Int(summary, "cart_count") != 302 {
		t.Fatalf("summary truncated to bounded list: %+v", summary)
	}
	ids, _ := json.Marshal(summary["favorite_ids"])
	var bounded []string
	_ = json.Unmarshal(ids, &bounded)
	if len(bounded) != 100 {
		t.Fatalf("favorite identity projection should remain bounded: %d", len(bounded))
	}
}
func TestStorefrontSectionsRejectGenericCommandsAndPrivateSummaryGuests(t *testing.T) {
	c := &aggregationCaller{}
	for _, section := range []string{"shell", "products", "activities", "bootstrap", "account_summary"} {
		code, _ := sectionHTTP(
			t,
			c,
			section,
			"buyer",
			map[string]any{"requests": []any{readItem("x", "catalog", "list", "")}},
		)
		if code != 400 {
			t.Fatalf("%s accepted a command list: %d", section, code)
		}
	}
	if code, _ := sectionHTTP(t, c, "batch", "buyer", map[string]any{"requests": []any{readItem("x", "catalog", "list", "")}}); code != 403 {
		t.Fatalf("generic batch still exposed: %d", code)
	}
	if code, _ := sectionHTTP(t, c, "account_summary", "", nil); code != 401 {
		t.Fatalf("guest private summary: %d", code)
	}
}
