package httpgateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCouponCenterPublicBrowseAndAuthenticatedClaim(t *testing.T) {
	for _, tc := range []struct {
		name, method, action, token string
		want                        int
		user                        string
	}{
		{"guest browsing", http.MethodPost, "templates", "", 200, ""},
		{"guest GET browsing", http.MethodGet, "templates", "", 200, ""},
		{"personalized browsing", http.MethodPost, "templates", "buyer-session", 200, "buyer"},
		{"invalid session", http.MethodPost, "templates", "invalid-session", 401, ""},
		{"claim needs session", http.MethodPost, "claim", "", 401, ""},
		{"wallet needs session", http.MethodPost, "list", "", 401, ""},
		{"authenticated claim", http.MethodPost, "claim", "buyer-session", 200, "buyer"},
		{"GET cannot claim", http.MethodGet, "claim", "buyer-session", 400, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &demoPaymentCaller{}
			req := httptest.NewRequest(
				tc.method,
				"/api/v1/coupon/"+tc.action,
				strings.NewReader(`{"user_id":"victim","role":"admin","data":{"template_id":"welcome-100"}}`),
			)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			response := httptest.NewRecorder()
			NewHandler(caller, false, t.TempDir()).ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if tc.want != 200 {
				if len(caller.forwarded) != 0 {
					t.Fatalf("blocked request reached coupon: %#v", caller.forwarded)
				}
				return
			}
			if len(caller.forwarded) != 1 || caller.forwarded[0].UserID != tc.user ||
				caller.forwarded[0].Action != tc.action {
				t.Fatalf("incorrect coupon principal or action: %#v", caller.forwarded)
			}
			if role := caller.forwarded[0].Role; (tc.user == "" && role != "") || (tc.user != "" && role != "user") {
				t.Fatalf("forged role forwarded: %q", role)
			}
		})
	}
}
