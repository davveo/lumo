package usecase

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/checkout/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestQuoteMainSubtotalCompatibilityIsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    map[string]any
		want int64
		fail bool
	}{
		{"legacy ordinary", map[string]any{"subtotal_cents": 3000}, 3000, false},
		{"explicit zero", map[string]any{"subtotal_cents": 3000, "main_subtotal_cents": 0}, 0, false},
		{"invalid main", map[string]any{"subtotal_cents": 3000, "main_subtotal_cents": 3001}, 0, true},
		{"negative main", map[string]any{"subtotal_cents": 3000, "main_subtotal_cents": -1}, 0, true},
		{"legacy missing addon line", map[string]any{"subtotal_cents": 3000, "addon_sku_id": "addon"}, 0, true},
		{"legacy ambiguous addon quantity", map[string]any{
			"subtotal_cents": 3000, "addon_sku_id": "same",
			"items": []any{map[string]any{"sku_id": "same", "quantity": 3, "unit_price_cents": 1000}},
		}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := quoteMainSubtotal(tc.q)
			if got != tc.want || (err != nil) != tc.fail {
				t.Fatalf("main subtotal=%d err=%v, want=%d fail=%v", got, err, tc.want, tc.fail)
			}
			if tc.fail && status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("invalid stored quote must require requote: %v", err)
			}
		})
	}
}

func TestPreviouslyIssuedQuotesCannotBypassBenefitRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]any
		fail bool
	}{
		{"flash coupon", map[string]any{"activity_kind": "flash", "coupon_id": "owned"}, true},
		{"group points", map[string]any{"activity_kind": "group", "points": 1}, true},
		{"legacy group coupon", map[string]any{"group_id": "team", "coupon_id": "owned"}, true},
		{"addon points too high", map[string]any{"points": 201, "subtotal_cents": 3000, "main_subtotal_cents": 2000}, true},
		{"addon points at limit", map[string]any{"points": 200, "subtotal_cents": 3000, "main_subtotal_cents": 2000}, false},
		{"legacy ordinary points", map[string]any{"points": 300, "subtotal_cents": 3000}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateQuoteBenefits(tc.data)
			if (err != nil) != tc.fail || tc.fail && status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("quote benefit compatibility error=%v fail=%v", err, tc.fail)
			}
		})
	}
}

type couponQuotePeers struct {
	quote          platform.Result
	couponSubtotal int64
	reservations   int
}

func (p *couponQuotePeers) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	switch service + "/" + c.Action {
	case "pricing/get":
		return p.quote, nil
	case "catalog/batch_get":
		return platform.Result{"items": []platform.Result{{"id": "same", "active": true, "price_cents": 1000}}}, nil
	case "campaign/evaluate":
		return platform.Result{"eligible": true, "discount_cents": 500, "policy_version": "test-v1"}, nil
	case "order/create", "order/pending", "inventory/reserve", "campaign/reserve":
		return platform.Result{}, nil
	case "coupon/reserve":
		p.reservations++
		p.couponSubtotal = platform.Int(c.Data, "subtotal_cents")
		return platform.Result{"discount_cents": 100}, nil
	}
	return nil, status.Error(codes.Unimplemented, "unexpected checkout test peer")
}

func TestCouponReservationUsesMainSubtotalIncludingLegacySameSKUAddon(t *testing.T) {
	for _, mode := range []string{"explicit main", "legacy addon", "legacy ordinary"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			q := platform.Result{
				"quote_id": "quote-owned", "user_id": "buyer", "coupon_id": "coupon-owned",
				"subtotal_cents": 3000, "payable_cents": 2400, "coupon_discount_cents": 100,
				"campaign_discount_cents": 500, "campaign_policy_version": "test-v1", "activity_id": "exchange",
				"addon_sku_id": "same", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
				"items": []platform.Result{
					{"sku_id": "same", "quantity": 2, "unit_price_cents": 1000},
					{"sku_id": "same", "quantity": 1, "unit_price_cents": 1000},
				},
			}
			want := int64(2000)
			if mode == "explicit main" {
				q["main_subtotal_cents"] = 2000
			} else if mode == "legacy ordinary" {
				delete(q, "addon_sku_id")
				delete(q, "activity_id")
				want = 3000
			}
			peers := &couponQuotePeers{quote: q}
			db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "checkout.db"))
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := db.DB()
			t.Cleanup(func() { _ = raw.Close() })
			repo := repository.New(db)
			if err = repo.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			_, err = New(repo, peers).Submit(ctx, platform.Request{
				UserID: "buyer", Role: "user", IdempotencyKey: "coupon-main-amount",
				Data: map[string]any{"quote_id": "quote-owned", "address": map[string]any{
					"recipient": "Buyer", "phone": "13800000000", "address": "Example street",
				}},
			})
			if err != nil || peers.reservations != 1 || peers.couponSubtotal != want {
				t.Fatalf(
					"coupon reservation used addon/incorrect amount: subtotal=%d want=%d calls=%d err=%v",
					peers.couponSubtotal,
					want,
					peers.reservations,
					err,
				)
			}
		})
	}
}
