package httpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type popupTestCaller struct {
	mu    sync.Mutex
	calls []aggregationCall
	fail  bool
}

func (c *popupTestCaller) Call(ctx context.Context, service string, command platform.Command) (platform.Result, error) {
	c.mu.Lock()
	c.calls = append(c.calls, aggregationCall{service, command})
	c.mu.Unlock()
	if service == "iam" {
		role := "user"
		if platform.Text(command.Data, "token") == "admin" {
			role = "admin"
		}
		return platform.Result{"user_id": "buyer", "role": role, "registered_at": "2026-01-01T00:00:00Z"}, nil
	}
	if service == "member" {
		return platform.Result{"growth": 1350, "level": "gold", "preferences": platform.Result{"categories": []string{"家居生活"}}}, nil
	}
	if service == "points" {
		return platform.Result{"available": 6500}, nil
	}
	if service == "catalog" && command.Action == "list" {
		return platform.Result{"items": []platform.Result{{"id": "owned-history", "category": "手机数码"}, {"id": "foreign-history", "category": "食品生鲜"}}}, nil
	}
	if service == "engagement" && platform.Text(command.Data, "kind") == "history" {
		return platform.Result{"items": []platform.Result{{"user_id": "buyer", "sku_id": "owned-history"}, {"user_id": "victim", "sku_id": "foreign-history"}}}, nil
	}
	if service == "content" && command.Action == "popup_decide" {
		if c.fail {
			return nil, status.Error(codes.Unavailable, "content down")
		}
		return platform.Result{"popup": platform.Result{"delivery_id": "delivery-test", "content": platform.Result{"title": "专属好礼"}}}, nil
	}
	if service == "content" && (command.Action == "popup_event" || command.Action == "popup_claim") {
		return platform.Result{"allowed": true}, nil
	}
	return platform.Result{"items": []any{}}, nil
}
func popupRequest(t *testing.T, handler http.Handler, action, token string, data map[string]any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(platform.Command{UserID: "victim", Role: "admin", Data: data})
	request := httptest.NewRequest(http.MethodPost, "https://shop.example/api/v1/"+action, bytes.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func popupCalls(c *popupTestCaller, service, action string) []platform.Command {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []platform.Command{}
	for _, call := range c.calls {
		if call.service == service && call.command.Action == action {
			out = append(out, call.command)
		}
	}
	return out
}
func popupBody(t *testing.T, response *httptest.ResponseRecorder) platform.Result {
	t.Helper()
	var out platform.Result
	if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return platform.Map(out["data"])
}

func TestPopupCookiesAreOpaqueStableAndNeverAuthenticate(t *testing.T) {
	caller := &popupTestCaller{}
	handler := NewHandler(caller, false, t.TempDir())
	response := popupRequest(t, handler, "storefront/popup", "", nil, nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("missing browser identity cookies: %v", cookies)
	}
	for _, cookie := range cookies {
		if !validPopupCookie(cookie.Value) || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatal("unsafe identity cookie", cookie.Name)
		}
		if cookie.Name == popupSessionCookie && cookie.MaxAge != 0 {
			t.Fatal("browser session must not persist", cookie.Name)
		}
	}
	first := popupCalls(caller, "content", "popup_decide")[0]
	if first.UserID != "" || first.Role != "system" || len(platform.Map(first.Data["profile"])) != 0 {
		t.Fatal("cookie authenticated an account", first)
	}
	response = popupRequest(t, handler, "storefront/popup", "", nil, cookies)
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("valid browser identity was rotated")
	}
	second := popupCalls(caller, "content", "popup_decide")[1]
	if first.Data["visitor_id"] != second.Data["visitor_id"] || first.Data["session_id"] != second.Data["session_id"] {
		t.Fatal("frequency scope changed")
	}
	response = popupRequest(t, handler, "storefront/popup_claim", "", map[string]any{"delivery_id": "d", "view_id": "v"}, cookies)
	if response.Code != 401 || len(popupCalls(caller, "content", "popup_claim")) != 0 {
		t.Fatal("cookie-only claim allowed")
	}
	response = popupRequest(t, handler, "storefront/popup", "", nil, []*http.Cookie{{Name: popupVisitorCookie, Value: "forged"}, {Name: popupSessionCookie, Value: strings.Repeat("g", 64)}})
	if len(response.Result().Cookies()) != 2 {
		t.Fatal("malformed cookie identity accepted")
	}
}
func TestBootstrapPopupUsesTrustedExistingFragmentsWithoutRepeatingProfileReads(t *testing.T) {
	caller := &popupTestCaller{}
	handler := NewHandler(caller, false, t.TempDir())
	response := popupRequest(t, handler, "storefront/bootstrap", "buyer", map[string]any{"placement": "home", "profile": map[string]any{"registered_at": "2099-01-01T00:00:00Z", "points": 999999}, "visitor_id": "forged"}, nil)
	if response.Code != 200 || popupBody(t, response)["popup"] == nil {
		t.Fatal(response.Body.String())
	}
	calls := popupCalls(caller, "content", "popup_decide")
	if len(calls) != 1 {
		t.Fatal(calls)
	}
	command := calls[0]
	profile := platform.Map(command.Data["profile"])
	if command.UserID != "buyer" || command.Role != "system" || profile["registered_at"] != "2026-01-01T00:00:00Z" || platform.Int(profile, "growth") != 1350 || platform.Int(profile, "points") != 6500 || profile["level"] != "gold" {
		t.Fatalf("forged or incomplete targeting profile: %v", command)
	}
	values := popupStringList(profile["interest_categories"])
	if strings.Join(values, ",") != "家居生活,手机数码" {
		t.Fatalf("other user's interests included: %v", values)
	}
	for _, service := range []string{"member", "points"} {
		if len(popupCalls(caller, service, "get")) != 1 {
			t.Fatalf("bootstrap repeated %s read", service)
		}
	}
	if len(popupCalls(caller, "content", "popup_event")) != 0 {
		t.Fatal("decision counted an impression")
	}
	caller.fail = true
	response = popupRequest(t, handler, "storefront/bootstrap", "buyer", nil, response.Result().Cookies())
	if response.Code != 200 || popupBody(t, response)["popup"] != nil {
		t.Fatal("marketing failure broke shopping", response.Body.String())
	}
}
func TestPopupEndpointsEnforceBoundaryAndPrincipal(t *testing.T) {
	for _, test := range []struct {
		path, token string
		data        map[string]any
		code        int
	}{
		{"storefront/popup", "", map[string]any{"profile": map[string]any{"points": 99999}}, 400},
		{"storefront/popup_event", "buyer", map[string]any{"event": "impression", "delivery_id": "d", "view_id": "v", "user_id": "victim"}, 400},
		{"storefront/popup", "", map[string]any{"placement": "checkout"}, 400},
		{"storefront/popup_event", "", map[string]any{"event": "claim", "delivery_id": "d", "view_id": "v"}, 400},
		{"content/popup_decide", "admin", nil, 403}, {"content/popup_event", "admin", nil, 403}, {"content/popup_claim", "admin", nil, 403},
		{"coupon/templates_by_ids", "admin", map[string]any{"ids": []string{"welcome-100"}}, 403},
		{"content/popup_list", "buyer", nil, 403}, {"content/popup_list", "", nil, 401}, {"content/popup_list", "admin", nil, 200},
		{"content/popup_upsert", "admin", nil, 200}, {"content/popup_configure", "admin", nil, 200},
	} {
		t.Run(test.path+test.token, func(t *testing.T) {
			caller := &popupTestCaller{}
			response := popupRequest(t, NewHandler(caller, false, t.TempDir()), test.path, test.token, test.data, nil)
			if response.Code != test.code {
				t.Fatalf("%d: %s", response.Code, response.Body.String())
			}
		})
	}
	caller := &popupTestCaller{}
	handler := NewHandler(caller, false, t.TempDir())
	for _, action := range []string{"popup_event", "popup_claim"} {
		response := popupRequest(t, handler, "storefront/"+action, "buyer", map[string]any{"delivery_id": "delivery-1", "view_id": "view-1", "event": "impression"}, nil)
		if action == "popup_claim" {
			response = popupRequest(t, handler, "storefront/"+action, "buyer", map[string]any{"delivery_id": "delivery-1", "view_id": "view-1"}, nil)
		}
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
		command := popupCalls(caller, "content", action)[0]
		if command.UserID != "buyer" || command.Role != "system" || command.Data["delivery_id"] != "delivery-1" || command.Data["profile"] != nil || !validPopupCookie(platform.Text(command.Data, "visitor_id")) {
			t.Fatal("principal or delivery changed", command)
		}
	}
	for _, token := range []string{"admin", "buyer"} {
		data := map[string]any{"placement": "none"}
		if token == "admin" {
			data = nil
		}
		response := popupRequest(t, handler, "storefront/popup", token, data, nil)
		if response.Code != 200 || popupBody(t, response)["popup"] != nil {
			t.Fatal("suppressed placement/account got popup")
		}
	}
	if len(popupCalls(caller, "content", "popup_decide")) != 0 {
		t.Fatal("suppressed decision reached content")
	}
}
func TestPopupPrivateCommandsCannotHideInReadBatch(t *testing.T) {
	for _, item := range []readRequest{
		{Service: "content", Action: "popup_decide"}, {Service: "content", Action: "popup_event"}, {Service: "content", Action: "popup_claim"}, {Service: "content", Action: "popup_upsert"}, {Service: "content", Action: "popup_configure"}, {Service: "coupon", Action: "templates_by_ids"}, {Service: "storefront", Action: "popup"},
	} {
		if err := authorizeRead(item, platform.Command{UserID: "admin", Role: "admin"}); status.Code(err) != codes.PermissionDenied {
			t.Fatal("write/private action batched", item, err)
		}
	}
	if err := authorizeRead(readRequest{Service: "content", Action: "popup_list"}, platform.Command{UserID: "admin", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
}

func TestPopupCookiesDoNotEnableCrossOriginOrGETMutations(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://frontend.example")
	caller := &popupTestCaller{}
	handler := NewHandler(caller, false, t.TempDir())
	first := popupRequest(t, handler, "storefront/popup", "", nil, nil)
	cookies := first.Result().Cookies()
	for _, origin := range []string{"https://evil.example", "null"} {
		request := httptest.NewRequest(http.MethodPost, "https://shop.example/api/v1/storefront/popup_event", strings.NewReader(`{"data":{"delivery_id":"delivery-test","view_id":"v","event":"impression"}}`))
		request.Header.Set("Origin", origin)
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || len(popupCalls(caller, "content", "popup_event")) != 0 || len(response.Result().Cookies()) != 0 {
			t.Fatalf("cross-origin request reached identity/domain handling: %d %s", response.Code, response.Body.String())
		}
	}
	// The configured external frontend may use explicit Bearer headers, but the
	// gateway never opts into browser cross-origin cookie credentials.
	request := httptest.NewRequest(http.MethodOptions, "https://shop.example/api/v1/storefront/popup", nil)
	request.Header.Set("Origin", "https://frontend.example")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "content-type,authorization")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("cross-origin credential policy changed")
	}
	for _, action := range []string{"popup", "popup_event", "popup_claim"} {
		request := httptest.NewRequest(http.MethodGet, "https://shop.example/api/v1/storefront/"+action+"?event=impression&delivery_id=delivery-test&view_id=v", nil)
		request.Header.Set("Authorization", "Bearer buyer")
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("GET %s was allowed: %d", action, response.Code)
		}
	}
	if len(popupCalls(caller, "content", "popup_event")) != 0 || len(popupCalls(caller, "content", "popup_claim")) != 0 {
		t.Fatal("GET caused popup mutation")
	}
}

func TestPopupInterestQualificationUsesOneBoundedCatalogBatch(t *testing.T) {
	caller := &aggregationCaller{handle: func(_ context.Context, service string, command platform.Command) (platform.Result, error) {
		if service != "catalog" || command.Action != "batch_get" || command.UserID != "" || command.Role != "" {
			t.Errorf("interest qualification performed unrelated or per-SKU read: %s/%s", service, command.Action)
		}
		var input struct {
			IDs []string `json:"ids"`
		}
		if err := platform.Decode(command.Data, &input); err != nil || len(input.IDs) != 50 {
			t.Errorf("interest batch was not bounded: %d %v", len(input.IDs), err)
		}
		items := []platform.Result{}
		for _, id := range input.IDs {
			items = append(items, platform.Result{"id": id, "category": "手机数码"})
		}
		return platform.Result{"items": items}, nil
	}}
	rows := []platform.Result{}
	for index := 0; index < 100; index++ {
		rows = append(rows, platform.Result{"user_id": "buyer", "sku_id": strings.Repeat("a", index+1)})
	}
	rows = append([]platform.Result{{"user_id": "victim", "sku_id": "foreign-id"}}, rows...)
	request := readRequest{Service: "engagement", Action: "list", Command: readCommand{Data: map[string]any{"kind": "history"}}, auth: true}
	fragments := []platform.Result{readResult(request, platform.Result{"items": rows}, nil)}
	fragments = (&gateway{rpc: caller}).popupInterestProducts(context.Background(), fragments, platform.Command{UserID: "buyer", Role: "user"})
	if caller.count("catalog", "batch_get") != 1 || caller.count("catalog", "get") != 0 || len(caller.calls) != 1 {
		t.Fatal("qualification introduced N+1 or stock reads", caller.calls)
	}
	profile := popupProfile(context.Background(), platform.Command{UserID: "buyer"}, fragments)
	if values := popupStringList(profile["interest_categories"]); len(values) != 1 || values[0] != "手机数码" {
		t.Fatal("bounded product metadata did not resolve interest category", values)
	}
}
