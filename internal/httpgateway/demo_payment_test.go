package httpgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type demoPaymentCaller struct{ forwarded []platform.Command }

func (c *demoPaymentCaller) Call(_ context.Context, service string, cmd platform.Command) (platform.Result, error) {
	if service == "iam" {
		if platform.Text(cmd.Data, "token") != "buyer-session" {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		return platform.Result{"user_id": "buyer", "role": "user"}, nil
	}
	c.forwarded = append(c.forwarded, cmd)
	return platform.Result{"id": cmd.ID, "status": "SUCCEEDED"}, nil
}

func TestDemoPaymentSubmitGatewayRequiresDemoAndBuyerSession(t *testing.T) {
	for _, tc := range []struct {
		name, method, action, token string
		demo                        bool
		want                        int
	}{
		{"buyer simulation", http.MethodPost, "submit_demo", "buyer-session", true, http.StatusOK},
		{"demo disabled", http.MethodPost, "submit_demo", "buyer-session", false, http.StatusForbidden},
		{"no session", http.MethodPost, "submit_demo", "", true, http.StatusUnauthorized},
		{"invalid session", http.MethodPost, "submit_demo", "invalid-session", true, http.StatusUnauthorized},
		{"mutation cannot use GET", http.MethodGet, "submit_demo", "buyer-session", true, http.StatusBadRequest},
		{"administrator confirmation remains restricted", http.MethodPost, "confirm_demo", "buyer-session", true, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &demoPaymentCaller{}
			request := httptest.NewRequest(tc.method, "/api/v1/payment/"+tc.action, strings.NewReader(`{"id":"pay-test","user_id":"victim","role":"admin"}`))
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+tc.token)
			}
			response := httptest.NewRecorder()
			NewHandler(caller, tc.demo, t.TempDir()).ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("got %d: %s", response.Code, response.Body.String())
			}
			if tc.want == http.StatusOK {
				if len(caller.forwarded) != 1 {
					t.Fatalf("wrong forwarded calls: %#v", caller.forwarded)
				}
				cmd := caller.forwarded[0]
				if cmd.Action != "submit_demo" || cmd.ID != "pay-test" || cmd.UserID != "buyer" || cmd.Role != "user" {
					t.Fatalf("forged browser principal reached payment: %#v", cmd)
				}
			} else if len(caller.forwarded) != 0 {
				t.Fatalf("blocked action reached payment: %#v", caller.forwarded)
			}
		})
	}
}
