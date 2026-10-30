package inventory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale"
	"github.com/davveo/lumo/services/fulfillment"
	"github.com/davveo/lumo/services/helpdesk"
	"github.com/davveo/lumo/services/inventory"
	"github.com/davveo/lumo/services/invoice"
	"github.com/davveo/lumo/services/logistics"
	"github.com/davveo/lumo/services/notification"
	"github.com/davveo/lumo/services/payment"
	"github.com/davveo/lumo/services/reconciliation"
	"github.com/davveo/lumo/services/refund"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Independently registered operations are used directly by named gRPC methods.
// Authentication and transport validation must survive bypassing legacy Handle.
func TestIndependentOperationsKeepWireContractsAndAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, actions, protected string
		factory                  func(platform.Dependencies) platform.Module
		denied                   codes.Code
	}{
		{"inventory", "get batch_get reserve confirm cancel dispatch restock adjust adjustments admin_list threshold ledger", "reserve", inventory.New, codes.Unauthenticated},
		{"fulfillment", "list get create_exchange dispatch cancel inspect_return admin_list admin_summary", "dispatch", fulfillment.New, codes.PermissionDenied},
		{"logistics", "quote list create get deliver policy_get policy_upsert", "create", logistics.New, codes.PermissionDenied},
		{"aftersale", "create get list approve reject cancel return inspect refund exchange admin_list admin_summary", "create", aftersale.New, codes.Unauthenticated},
		{"payment", "get list create submit_demo confirm_demo close admin_list admin_summary methods policy_get policy_upsert provider_get provider_upsert", "create", payment.New, codes.Unauthenticated},
		{"refund", "get list create confirm_demo admin_list admin_summary", "create", refund.New, codes.Unauthenticated},
		{"invoice", "list create get reversals issue_demo adjust_demo admin_list admin_summary", "create", invoice.New, codes.Unauthenticated},
		{"reconciliation", "list run admin_list get note", "list", reconciliation.New, codes.Unauthenticated},
		{"notification", "list read read_all templates_admin template_upsert", "list", notification.New, codes.Unauthenticated},
		{"helpdesk", "faq list get create reply close admin_list", "create", helpdesk.New, codes.Unauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := tc.factory(platform.Dependencies{})
			actions := strings.Fields(tc.actions)
			if len(module.Operations) != len(actions) {
				t.Fatalf("registered %d operations, expected %d", len(module.Operations), len(actions))
			}
			for _, action := range actions {
				if module.Operations[action] == nil {
					t.Errorf("missing independent method for %s", action)
				}
			}
			if _, err := module.Operations[tc.protected](context.Background(), platform.Request{}); status.Code(
				err,
			) != tc.denied {
				t.Fatalf("direct operation bypassed authorization: %v", err)
			}
		})
	}
}

func TestIndependentFinancialMethodsKeepIdentifierValidation(t *testing.T) {
	for _, factory := range []func(platform.Dependencies) platform.Module{payment.New, refund.New, reconciliation.New} {
		module := factory(platform.Dependencies{})
		for action, operation := range module.Operations {
			for _, request := range []platform.Request{{ID: strings.Repeat("x", 161)}, {IdempotencyKey: strings.Repeat("x", 161)}} {
				if _, err := operation(context.Background(), request); status.Code(err) != codes.InvalidArgument {
					t.Errorf("%s/%s skipped identifier validation: %v", module.Name, action, err)
				}
			}
		}
	}
}
