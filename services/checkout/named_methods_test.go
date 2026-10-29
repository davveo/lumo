package checkout_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	checkoutmod "github.com/davveo/lumo/services/checkout"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type namedWorkflowPeers struct {
	t     *testing.T
	calls []string
	group bool
	fail  string
}

func (p *namedWorkflowPeers) Call(context.Context, string, platform.Command) (platform.Result, error) {
	p.t.Fatal("checkout used the action-based caller instead of a named method")
	return nil, nil
}

func (p *namedWorkflowPeers) Invoke(
	_ context.Context,
	service, method string,
	r platform.Request,
) (platform.Result, error) {
	p.t.Helper()
	p.calls = append(p.calls, service+"."+method)
	if r.UserID != "demo" {
		p.t.Fatalf("lost checkout principal: %#v", r)
	}
	switch service + "." + method {
	case "pricing.Get":
		if r.ID != "quote-methods" || r.Role != constants.RoleUser {
			p.t.Fatalf("wrong quote request: %#v", r)
		}
		quote := platform.Result{
			"quote_id": "quote-methods", "user_id": "demo",
			"expires_at":     time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
			"subtotal_cents": int64(1000), "payable_cents": int64(850),
			"activity_id": "activity-methods", "campaign_policy_version": "v1",
			"coupon_id":             "coupon-methods",
			"coupon_discount_cents": int64(100), "points": int64(50),
			"items": []platform.Result{{"sku_id": "sku-test", "quantity": int64(1), "unit_price_cents": int64(1000)}},
		}
		if p.group {
			quote["group_id"], quote["activity_kind"] = "group-methods", constants.CampaignKindGroup
			delete(quote, "coupon_id")
			quote["points"], quote["coupon_discount_cents"], quote["payable_cents"] = int64(0), int64(0), int64(1000)
		}
		return quote, nil
	case "catalog.BatchGet":
		return platform.Result{
			"items": []platform.Result{{"id": "sku-test", "price_cents": int64(1000), "active": true}},
		}, nil
	case "campaign.Evaluate":
		return platform.Result{"eligible": true, "policy_version": "v1", "discount_cents": int64(0)}, nil
	}
	if r.Role != constants.RoleSystem || r.ID == "" {
		p.t.Fatalf("lost internal workflow identity: %s.%s %#v", service, method, r)
	}
	if method == "Reserve" {
		if platform.Text(r.Data, "order_id") != r.ID {
			p.t.Fatalf("reserve lost order key: %#v", r)
		}
		if service == p.fail {
			return nil, status.Error(codes.DeadlineExceeded, "reservation response lost")
		}
		if service == constants.ServiceCoupon && platform.Int(r.Data, "subtotal_cents") != 1000 {
			p.t.Fatalf("coupon reserve lost quote amount: %#v", r)
		}
		return platform.Result{"discount_cents": int64(100)}, nil
	}
	if service == constants.ServiceOrder && method == "Get" {
		return platform.Result{"status": constants.OrderStatusPaidConfirming, "user_id": r.UserID}, nil
	}
	if service == constants.ServiceGroup && method == "Paid" {
		return platform.Result{"status": constants.GroupStatusFormed}, nil
	}
	return platform.Result{}, nil
}

func TestNamedWorkflowMethodsKeepResourceOrderAndCompensation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		group bool
		fail  string
	}{
		{"confirm ordinary benefits", false, ""},
		{"compensate ordinary benefits", false, constants.ServicePoints},
		{"confirm group", true, ""},
		{"compensate group", true, constants.ServiceGroup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "checkout.db"))
			if err != nil {
				t.Fatal(err)
			}
			sql, _ := db.DB()
			t.Cleanup(func() { _ = sql.Close() })
			p := &namedWorkflowPeers{t: t, group: tc.group, fail: tc.fail}
			m := checkoutmod.New(platform.Dependencies{DB: db, RPC: p})
			if err = m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			req := platform.Request{
				UserID: "demo", Role: constants.RoleUser, IdempotencyKey: "named-workflow",
				Data: map[string]any{
					"quote_id": "quote-methods",
					"address":  map[string]any{"recipient": "Demo", "phone": "00000000000", "address": "Demo street"},
				},
			}
			result, err := m.Operations[constants.ActionSubmit](ctx, req)
			want := []string{
				"pricing.Get", "catalog.BatchGet", "campaign.Evaluate", "order.Create",
				"inventory.Reserve", "campaign.Reserve",
			}
			if tc.group {
				want = append(want, "group.Reserve")
			} else {
				want = append(want, "coupon.Reserve", "points.Reserve")
			}
			if tc.fail != "" {
				if status.Code(err) != codes.DeadlineExceeded {
					t.Fatalf("lost original reservation failure: %v", err)
				}
				if tc.group {
					want = append(want, "group.Cancel")
				} else {
					want = append(want, "points.Cancel", "coupon.Cancel")
				}
				want = append(want, "campaign.Cancel", "inventory.Cancel", "order.CreateFailed")
				result, err = m.Operations[constants.ActionSubmit](ctx, req)
				if err != nil || platform.Text(result, "status") != constants.CheckoutStateCancelled {
					t.Fatalf("compensation was not durable: %#v, %v", result, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, "order.Pending")
				_, err = m.Operations[constants.ActionPaid](ctx, platform.Request{
					ID: platform.Text(result, "order_id"), UserID: "demo", Role: constants.RoleSystem,
				})
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, "order.Get")
				if !tc.group {
					want = append(want, "coupon.Confirm", "points.Confirm")
				}
				want = append(want, "campaign.Confirm")
				if tc.group {
					want = append(want, "group.Paid", "order.GroupFormed")
				}
				want = append(want, "inventory.Confirm", "order.Ready")
			}
			if !reflect.DeepEqual(p.calls, want) {
				t.Fatalf("workflow methods/order changed:\n got %v\nwant %v", p.calls, want)
			}
		})
	}
}
