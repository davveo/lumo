package httpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type aggregationCall struct {
	service string
	command platform.Command
}
type aggregationCaller struct {
	mu     sync.Mutex
	calls  []aggregationCall
	handle func(context.Context, string, platform.Command) (platform.Result, error)
}

func (c *aggregationCaller) Call(ctx context.Context, service string, cmd platform.Command) (platform.Result, error) {
	c.mu.Lock()
	c.calls = append(c.calls, aggregationCall{service, cmd})
	c.mu.Unlock()
	if service == "iam" {
		role := "user"
		if platform.Text(cmd.Data, "token") == "admin" {
			role = "admin"
		}
		return platform.Result{"user_id": "buyer", "role": role}, nil
	}
	if c.handle != nil {
		return c.handle(ctx, service, cmd)
	}
	return platform.Result{"items": []any{}}, nil
}
func (c *aggregationCaller) count(service, action string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, call := range c.calls {
		if call.service == service && call.command.Action == action {
			count++
		}
	}
	return count
}
func batchHTTP(t *testing.T, caller platform.Caller, action, token string, requests any) (int, platform.Result) {
	t.Helper()
	data := map[string]any{}
	if requests != nil {
		data["requests"] = requests
	}
	body, _ := json.Marshal(platform.Command{Data: data})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/storefront/"+action, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	NewHandler(caller, false, t.TempDir()).ServeHTTP(w, req)
	var out platform.Result
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return w.Code, out
}
func readItem(key, service, action, id string) map[string]any {
	return map[string]any{"key": key, "service": service, "action": action, "command": map[string]any{"id": id, "data": map[string]any{}}}
}

func TestReadBatchPreflightsAllCommandsBeforeAnyDomainCall(t *testing.T) {
	for _, tc := range []struct {
		name, token string
		items       []any
		want        int
	}{
		{"write hidden among reads", "", []any{readItem("a", "catalog", "list", ""), readItem("b", "inventory", "adjust", "sku")}, 403},
		{"quote persists a record", "buyer", []any{readItem("a", "catalog", "list", ""), readItem("b", "checkout", "quote", "")}, 403},
		{"admin cannot batch writes", "admin", []any{readItem("a", "catalog", "upsert", "sku")}, 403},
		{"guest private read", "", []any{readItem("a", "member", "get", "")}, 401},
		{"user admin read", "buyer", []any{readItem("a", "payment", "list", "")}, 403},
		{"nested aggregation", "", []any{readItem("a", "storefront", "batch", "")}, 403},
		{"internal recommendation ranking", "admin", []any{readItem("a", "recommendation", "rank_bootstrap", "")}, 403},
		{"internal catalog search", "admin", []any{readItem("a", "catalog", "search_products", "")}, 403},
		{"duplicate keys", "", []any{readItem("a", "catalog", "list", ""), readItem("a", "catalog", "get", "sku")}, 400},
		{"invalid SKU cannot poison a shared bulk read", "", []any{readItem("a", "catalog", "list", ""), readItem("b", "inventory", "get", " sku")}, 400},
		{"forged nested principal", "buyer", []any{map[string]any{"key": "a", "service": "member", "action": "get", "command": map[string]any{"user_id": "victim", "role": "admin"}}}, 400},
		{"idempotent writes cannot hide in a read", "buyer", []any{map[string]any{"key": "a", "service": "member", "action": "get", "command": map[string]any{"idempotency_key": "fake"}}}, 400},
		{"empty", "", []any{}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &aggregationCaller{}
			code, body := batchHTTP(t, caller, "batch", tc.token, tc.items)
			if code != tc.want {
				t.Fatalf("got %d want %d: %v", code, tc.want, body)
			}
			for _, call := range caller.calls {
				if call.service != "iam" {
					t.Fatalf("unvalidated batch reached %s/%s", call.service, call.command.Action)
				}
			}
		})
	}
	tooMany := []any{}
	for i := 0; i < 65; i++ {
		item := readItem(string(rune('a'+i)), "catalog", "list", "")
		tooMany = append(tooMany, item)
	}
	if code, _ := batchHTTP(t, &aggregationCaller{}, "batch", "", tooMany); code != 400 {
		t.Fatalf("oversized batch status %d", code)
	}
}

func TestReadBatchUsesOneBulkRPCAndKeepsPartialErrorsAndPrincipal(t *testing.T) {
	caller := &aggregationCaller{handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
		if c.UserID != "buyer" || c.Role != "user" {
			t.Errorf("principal changed: %#v", c)
		}
		if c.Action == "batch_get" {
			var payload struct {
				IDs []string `json:"ids"`
			}
			_ = platform.Decode(c.Data, &payload)
			rows := []platform.Result{}
			for _, id := range payload.IDs {
				if id != "missing" {
					field := "id"
					if service == "inventory" {
						field = "sku_id"
					}
					rows = append(rows, platform.Result{field: id, "available": 7})
				}
			}
			return platform.Result{"items": rows, "missing_ids": []string{"missing"}}, nil
		}
		if service == "content" {
			return nil, status.Error(codes.Unavailable, "content offline")
		}
		return platform.Result{"user_id": c.UserID}, nil
	}}
	items := []any{readItem("i1", "inventory", "get", "sku-a"), readItem("i2", "inventory", "get", "sku-a"), readItem("i3", "inventory", "get", "sku-b"), readItem("absent", "inventory", "get", "missing"), readItem("c", "catalog", "get", "sku-a"), readItem("m1", "member", "get", ""), readItem("m2", "member", "get", ""), readItem("error", "content", "list", "")}
	code, body := batchHTTP(t, caller, "batch", "buyer", items)
	if code != 200 {
		t.Fatalf("%d: %v", code, body)
	}
	results := resultRows(platform.Result{"items": platform.Map(body["data"])["results"]})
	if len(results) != len(items) {
		t.Fatalf("lost result rows: %v", body)
	}
	for i, row := range results {
		if row["key"] != items[i].(map[string]any)["key"] || row["auth"] != true {
			t.Fatalf("result order/auth lost: %v", row)
		}
	}
	if caller.count("inventory", "batch_get") != 1 || caller.count("inventory", "get") != 0 || caller.count("catalog", "batch_get") != 1 || caller.count("member", "get") != 1 {
		t.Fatalf("N+1 not collapsed: %+v", caller.calls)
	}
	if platform.Map(results[3]["error"])["code"] != "NotFound" || platform.Map(results[7]["error"])["code"] != "Unavailable" || results[0]["data"] == nil {
		t.Fatalf("partial failure discarded good reads: %v", results)
	}
}

func TestBootstrapIncludesPublicAndPersonalizedDataAndBulkSKUPrimer(t *testing.T) {
	caller := &aggregationCaller{handle: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
		if service == "catalog" && c.Action == "list" {
			if c.UserID != "" || c.Role != "" || platform.Int(c.Data, "limit") != 100 {
				t.Errorf("public catalog inherited privilege: %#v", c)
			}
			return platform.Result{"items": []platform.Result{{"id": "product", "variants": []platform.Result{{"sku_id": "variant-a"}, {"sku_id": "variant-b"}}}}}, nil
		}
		if service == "flash" {
			return platform.Result{"items": []platform.Result{{"sku_id": "flash-sku"}, {"sku_id": "flash-sku"}}}, nil
		}
		if service == "cart" {
			return platform.Result{"items": []platform.Result{{"sku_id": "cart-outside-catalog"}}}, nil
		}
		if c.Action == "batch_get" {
			if c.UserID != "" || c.Role != "" {
				t.Error("public SKU primer inherited principal")
			}
			var data struct {
				IDs []string `json:"ids"`
			}
			_ = platform.Decode(c.Data, &data)
			items := []platform.Result{}
			for _, id := range data.IDs {
				field := "id"
				if service == "inventory" {
					field = "sku_id"
				}
				items = append(items, platform.Result{field: id, "available": 10})
			}
			return platform.Result{"items": items}, nil
		}
		if service == "member" && c.UserID != "buyer" {
			t.Error("member read lost principal")
		}
		return platform.Result{"items": []any{}}, nil
	}}
	code, body := batchHTTP(t, caller, "bootstrap", "buyer", nil)
	if code != 200 {
		t.Fatalf("%d: %v", code, body)
	}
	results := resultRows(platform.Result{"items": platform.Map(body["data"])["results"]})
	stocks := map[string]bool{}
	private := false
	for _, row := range results {
		if row["service"] == "inventory" && row["action"] == "get" {
			command := platform.Map(row["command"])
			stocks[platform.Text(command, "id")] = true
			if row["auth"] != false {
				t.Error("public stock cache key is private")
			}
		}
		if row["service"] == "member" {
			private = row["auth"] == true
		}
	}
	if !private || len(stocks) != 5 || !stocks["variant-b"] || !stocks["flash-sku"] || !stocks["cart-outside-catalog"] {
		t.Fatalf("missing account/variant primer: %+v", results)
	}
	if caller.count("inventory", "batch_get") != 1 || caller.count("catalog", "batch_get") != 1 || caller.count("inventory", "get") != 0 || caller.count("catalog", "get") != 0 || caller.count("coupon", "templates") != 1 {
		t.Fatalf("bootstrap duplicated SKU/template reads: %+v", caller.calls)
	}
	guest := &aggregationCaller{}
	code, body = batchHTTP(t, guest, "bootstrap", "", nil)
	if code != 200 {
		t.Fatal(body)
	}
	for _, call := range guest.calls {
		if call.service == "recommendation" && call.command.Action == "rank_bootstrap" && call.command.Role == "system" && call.command.UserID == "" {
			continue
		}
		if call.service == "content" && call.command.Action == "popup_decide" && call.command.Role == "system" && call.command.UserID == "" {
			continue // delivery decision is internal, but contains no private account reads
		}
		if !publicActions[call.service+"/"+call.command.Action] {
			t.Fatalf("guest bootstrap performed private read: %+v", call)
		}
	}
}

func TestReadAggregationBoundsFanoutAndPropagatesCancellation(t *testing.T) {
	var mu sync.Mutex
	active, maximum := 0, 0
	caller := &aggregationCaller{handle: func(ctx context.Context, _ string, _ platform.Command) (platform.Result, error) {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		<-ctx.Done()
		mu.Lock()
		active--
		mu.Unlock()
		return nil, status.FromContextError(ctx.Err()).Err()
	}}
	requests := []readRequest{}
	for i := 0; i < 40; i++ {
		requests = append(requests, readRequest{Key: string(rune('a' + i)), Service: "content", Action: "get", Command: readCommand{ID: string(rune('a' + i)), Data: map[string]any{}}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	rows := (&gateway{rpc: caller}).runReads(ctx, requests, platform.Command{})
	if maximum > 8 || maximum < 1 || len(rows) != 40 {
		t.Fatalf("unbounded fanout: max=%d rows=%d", maximum, len(rows))
	}
	for _, row := range rows {
		if platform.Map(row["error"])["code"] != "DeadlineExceeded" {
			t.Fatalf("context lost: %v", row)
		}
	}
}
