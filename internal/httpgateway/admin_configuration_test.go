package httpgateway

import (
	"github.com/davveo/lumo/internal/constants"
	"net/http"
	"testing"
)

func TestConfigurationRoutesRequireRealAdministrator(t *testing.T) {
	for _, route := range []struct {
		service, action string
		read            bool
	}{
		{constants.ServiceCoupon, constants.ActionTemplatesAdmin, true},
		{constants.ServiceCoupon, constants.ActionTemplateUpsert, false},
		{constants.ServiceCampaign, constants.ActionAdminList, true},
		{constants.ServiceCampaign, constants.ActionUpsert, false},
	} {
		for _, session := range []string{"", "buyer", "admin"} {
			t.Run(route.service+"/"+route.action+"/"+session, func(t *testing.T) {
				caller := &popupTestCaller{}
				handler := NewHandler(caller, false, t.TempDir())
				response := popupRequest(
					t,
					handler,
					route.service+"/"+route.action,
					session,
					map[string]any{"active": false},
					nil,
				)
				want := http.StatusOK
				if session == "" {
					want = http.StatusUnauthorized
				} else if session != "admin" {
					want = http.StatusForbidden
				}
				if response.Code != want {
					t.Fatalf("status=%d want=%d: %s", response.Code, want, response.Body.String())
				}
				calls := popupCalls(caller, route.service, route.action)
				if session != "admin" {
					if len(calls) != 0 {
						t.Fatal("unauthorized configuration reached service")
					}
					return
				}
				if len(calls) != 1 || calls[0].Role != constants.RoleAdmin || calls[0].UserID != "buyer" {
					t.Fatalf("wrong authenticated principal: %#v", calls)
				}
				if route.read != reads[route.action] {
					t.Fatal("configuration read/write classification incorrect")
				}
			})
		}
	}
}
