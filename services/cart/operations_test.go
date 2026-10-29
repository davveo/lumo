package cart_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	analyticsmod "github.com/davveo/lumo/services/analytics"
	campaignmod "github.com/davveo/lumo/services/campaign"
	cartmod "github.com/davveo/lumo/services/cart"
	checkoutmod "github.com/davveo/lumo/services/checkout"
	couponmod "github.com/davveo/lumo/services/coupon"
	flashmod "github.com/davveo/lumo/services/flash"
	groupmod "github.com/davveo/lumo/services/group"
	pointsmod "github.com/davveo/lumo/services/points"
	pricingmod "github.com/davveo/lumo/services/pricing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A direct operation must enforce authorization itself: the independent gRPC
// methods bypass the legacy Command boundary and cannot rely on its dispatcher.
func TestIndependentOperationsKeepAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name      string
		build     func(platform.Dependencies) platform.Module
		count     int
		protected []string
	}{
		{constants.ServiceAnalytics, analyticsmod.New, 2, []string{constants.ActionRecord, constants.ActionList}},
		{constants.ServiceCart, cartmod.New, 6, []string{
			constants.ActionList, constants.ActionAdd, constants.ActionSet,
			constants.ActionRemove, constants.ActionClear, constants.ActionMerge,
		}},
		{constants.ServicePricing, pricingmod.New, 2, []string{constants.ActionQuote, constants.ActionGet}},
		{constants.ServiceCheckout, checkoutmod.New, 5, []string{
			constants.ActionQuote, constants.ActionSubmit, constants.ActionGet, constants.ActionPaid, constants.ActionCancel,
		}},
		{constants.ServiceCampaign, campaignmod.New, 8, []string{
			constants.ActionReserve, constants.ActionConfirm, constants.ActionCancel,
		}},
		{constants.ServiceCoupon, couponmod.New, 12, []string{
			constants.ActionList, constants.ActionClaim, constants.ActionCalculate, constants.ActionReserve,
			constants.ActionConfirm, constants.ActionCancel, constants.ActionRefund,
		}},
		{constants.ServicePoints, pointsmod.New, 12, []string{
			constants.ActionGet, constants.ActionCheckin, constants.ActionReserve, constants.ActionConfirm,
			constants.ActionCancel, constants.ActionRefund, constants.ActionReward,
		}},
		{constants.ServiceGroup, groupmod.New, 11, []string{
			constants.ActionCreate, constants.ActionReserve, constants.ActionPaid, constants.ActionCancel, constants.ActionExpire,
		}},
		{constants.ServiceFlash, flashmod.New, 7, []string{constants.ActionGet, constants.ActionRequest}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.build(platform.Dependencies{})
			if len(m.Operations) != tc.count {
				t.Fatalf("registered operations = %d, want %d", len(m.Operations), tc.count)
			}
			for _, action := range tc.protected {
				operation := m.Operations[action]
				if operation == nil {
					t.Fatalf("missing independent operation %q", action)
				}
				if _, err := operation(context.Background(), platform.Request{}); status.Code(
					err,
				) != codes.Unauthenticated {
					t.Errorf("direct %s authorization = %v", action, err)
				}
			}
		})
	}
}

type catalogAndInventory struct{ t *testing.T }

func (p catalogAndInventory) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	p.t.Helper()
	if c.Action != constants.ActionGet || c.ID != "sku-methods" || c.UserID != "member" ||
		c.Role != constants.RoleUser {
		p.t.Fatalf("lost request principal or SKU: %s %#v", service, c)
	}
	switch service {
	case constants.ServiceCatalog:
		return platform.Result{"id": c.ID}, nil
	case constants.ServiceInventory:
		return platform.Result{"available": int64(20)}, nil
	default:
		p.t.Fatalf("unexpected peer: %s", service)
		return nil, nil
	}
}

func TestCartAddAndSetAreIndependentMethods(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "cart.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { _ = sql.Close() })
	m := cartmod.New(platform.Dependencies{DB: db, RPC: catalogAndInventory{t}})
	if err = m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	req := platform.Request{
		UserID: "member", Role: constants.RoleUser,
		Data: map[string]any{"sku_id": "sku-methods", "quantity": int64(2), "action": constants.ActionSet},
	}
	call := func(action string, want int64) {
		t.Helper()
		result, callErr := m.Operations[action](ctx, req)
		if callErr != nil || platform.Int(result, "quantity") != want {
			t.Fatalf("%s = %#v, %v; want quantity %d", action, result, callErr, want)
		}
	}
	call(constants.ActionAdd, 2)
	req.Data["quantity"] = int64(3)
	call(constants.ActionAdd, 5)
	req.Data["quantity"] = int64(1)
	req.Data["action"] = constants.ActionAdd
	call(constants.ActionSet, 1)
	result, err := m.Operations[constants.ActionList](ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	var items struct {
		Items []platform.Result `json:"items"`
	}
	if err = platform.Decode(result, &items); err != nil || len(items.Items) != 1 ||
		platform.Int(items.Items[0], "quantity") != 1 {
		t.Fatalf("persisted cart = %#v, %v", result, err)
	}
	req.UserID = "other-member"
	result, err = m.Operations[constants.ActionList](ctx, req)
	if err != nil || platform.Int(result, "limit") != 0 {
		t.Fatalf("cart read leaked another member: %#v, %v", result, err)
	}
}
