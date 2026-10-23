package main

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeCaller struct {
	calls []platform.Command
	role  string
}

func (f *fakeCaller) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	f.calls = append(f.calls, c)
	if service == "iam" && c.Action == "validate" {
		if platform.Text(c.Data, "token") != "valid" {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		return platform.Result{"user_id": "real-user", "role": f.role}, nil
	}
	return platform.Result{"ok": true}, nil
}
func request(h http.Handler, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestBrowserCannotForgePrincipal(t *testing.T) {
	f := &fakeCaller{role: "user"}
	w := request(newHandler(f, true), "/api/v1/member/update", `{"user_id":"victim","role":"system","data":{"nickname":"safe"}}`, "valid")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	last := f.calls[len(f.calls)-1]
	if last.UserID != "real-user" || last.Role != "user" {
		t.Fatalf("forged principal forwarded: %#v", last)
	}
}
func TestInternalAndAdminActionsBlocked(t *testing.T) {
	f := &fakeCaller{role: "user"}
	h := newHandler(f, true)
	for _, p := range []string{"/api/v1/order/ready", "/api/v1/inventory/reserve", "/api/v1/refund/create", "/api/v1/payment/confirm_demo"} {
		w := request(h, p, `{"data":{}}`, "valid")
		if w.Code != 403 {
			t.Errorf("%s got %d", p, w.Code)
		}
	}
}
func TestUserRequiresSessionAndDemoGate(t *testing.T) {
	f := &fakeCaller{role: "admin"}
	h := newHandler(f, false)
	if w := request(h, "/api/v1/order/list", `{}`, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(h, "/api/v1/payment/confirm_demo", `{}`, "valid"); w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestPublicCatalogAndMalformedJSON(t *testing.T) {
	f := &fakeCaller{}
	h := newHandler(f, true)
	w := request(h, "/api/v1/catalog/list", `{"data":{}}`, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w := request(h, "/api/v1/catalog/list", `{} {}`, ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestIAMUsesBearerForCurrentRefreshAndLogout(t *testing.T) {
	for _, action := range []string{"current", "refresh", "logout"} {
		f := &fakeCaller{role: "user"}
		w := request(newHandler(f, true), "/api/v1/iam/"+action, `{"data":{"token":"forged-body-token"}}`, "valid")
		if w.Code != 200 {
			t.Fatal(action, w.Code, w.Body.String())
		}
		got := f.calls[len(f.calls)-1]
		wantAction := action
		if action == "current" {
			wantAction = "validate"
		}
		if got.Action != wantAction || platform.Text(got.Data, "token") != "valid" {
			t.Fatalf("%s forwarded browser token claims: %#v", action, got)
		}
	}
}

func TestAdminReadListsCannotBeReadByCustomer(t *testing.T) {
	h := newHandler(&fakeCaller{role: "user"}, true)
	for _, path := range []string{"/api/v1/payment/list", "/api/v1/refund/list", "/api/v1/reconciliation/list", "/api/v1/analytics/list"} {
		w := request(h, path, `{}`, "valid")
		if w.Code != 403 {
			t.Fatalf("%s returned%d", path, w.Code)
		}
	}
}

func TestHealthcheckURLUsesConfiguredListener(t *testing.T) {
	for _, tc := range []struct{ addr, want string }{
		{":8080", "http://127.0.0.1:8080/readyz"},
		{"127.0.0.1:18080", "http://127.0.0.1:18080/readyz"},
		{"0.0.0.0:8080", "http://127.0.0.1:8080/readyz"},
		{"localhost:8080", "http://localhost:8080/readyz"},
		{"[::]:8080", "http://[::1]:8080/readyz"},
		{"[::1]:18080", "http://[::1]:18080/readyz"},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			got, err := healthcheckURL(tc.addr)
			if err != nil || got != tc.want {
				t.Fatalf("healthcheckURL(%q) = %q, %v; want %q", tc.addr, got, err, tc.want)
			}
		})
	}
	if _, err := healthcheckURL("invalid-address"); err == nil {
		t.Fatal("invalid listener address was accepted")
	}
}

func TestHealthcheckRequiresReadyResponse(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/readyz" {
					t.Errorf("unexpected healthcheck path %q", r.URL.Path)
				}
				w.WriteHeader(code)
			}))
			defer srv.Close()
			t.Setenv("HTTP_ADDR", strings.TrimPrefix(srv.URL, "http://"))
			err := healthcheck()
			if (err == nil) != (code == http.StatusOK) {
				t.Fatalf("status %d: got %v", code, err)
			}
		})
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	t.Setenv("HTTP_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	if err := healthcheck(); err == nil {
		t.Fatal("healthcheck succeeded with no listener")
	}
}
