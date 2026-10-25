package cart_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/cart"
	"github.com/davveo/lumo/services/cart/internal/domain"
	"github.com/davveo/lumo/services/cart/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCartListReportsExactOwnedRowAndQuantityTotalsBeyondThePageLimit(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "counts.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	module := cart.New(platform.Dependencies{DB: db})
	if err = module.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	records := []domain.Record{}
	for _, fixture := range []struct {
		owner, kind, state string
		count, quantity    int
	}{
		{"buyer", constants.CartRecordKindCartItem, constants.CartStatusActive, 123, 0},
		{"buyer", constants.CartRecordKindCartItem, constants.CartStatusConsumed, 2, 7},
		{"buyer", constants.CartRecordKindCheckoutReceipt, constants.CartStatusConsumed, 119, 999},
		{"other", constants.CartRecordKindCartItem, constants.CartStatusActive, 137, 2},
	} {
		for index := 0; index < fixture.count; index++ {
			quantity := fixture.quantity
			if quantity == 0 {
				quantity = index%10 + 1
			}
			records = append(records, domain.Record{
				ID:        fmt.Sprintf("%s-%s-%s-%03d", fixture.owner, fixture.kind, fixture.state, index),
				UserID:    fixture.owner,
				Kind:      fixture.kind,
				Status:    fixture.state,
				Payload:   fmt.Sprintf(`{"sku_id":"sku-%03d","quantity":%d}`, index, quantity),
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			})
		}
	}
	for index, payload := range []string{`{"quantity":-3}`, `{}`, `invalid-json`, `{"quantity":"4"}`, `{"quantity":2}`, `{"quantity":null}`} {
		records = append(records, domain.Record{
			ID: fmt.Sprintf("legacy-%d", index), UserID: "legacy",
			Kind: constants.CartRecordKindCartItem, Status: constants.CartStatusActive, Payload: payload,
		})
	}
	if err = db.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, owner, role string
		limit, offset     int
		total, quantity   int64
		items             int
	}{
		{"default page", "buyer", "user", 0, 0, 125, 680, 20},
		{"first hundred", "buyer", "user", 100, 0, 125, 680, 100},
		{"last page", "buyer", "user", 100, 100, 125, 680, 25},
		{"single row", "buyer", "user", 1, 100, 125, 680, 1},
		{"past last page", "buyer", "user", 100, 125, 125, 680, 0},
		{"oversized limit remains bounded", "buyer", "user", 1000, 0, 125, 680, 20},
		{"negative offset remains first page", "buyer", "user", 100, -1, 125, 680, 100},
		{"other owner", "other", "user", 100, 0, 137, 274, 100},
		{"admin still reads own cart", "other", "admin", 100, 0, 137, 274, 100},
		{"empty account", "empty", "user", 100, 0, 0, 0, 0},
		{"legacy payloads", "legacy", "user", 100, 0, 6, 6, 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := module.Operations[constants.ActionList](ctx, platform.Request{
				UserID: test.owner, Role: test.role,
				Data: map[string]any{
					"limit": test.limit, "offset": test.offset,
					"user_id": "forged-user", "role": "admin",
					"kind": constants.CartRecordKindCheckoutReceipt, "status": constants.CartStatusConsumed,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Items         []platform.Result `json:"items"`
				Total         int64             `json:"total"`
				QuantityTotal int64             `json:"quantity_total"`
				Limit         int               `json:"limit"`
			}
			if err = platform.Decode(result, &response); err != nil {
				t.Fatal(err)
			}
			if response.Total != test.total || response.QuantityTotal != test.quantity ||
				len(response.Items) != test.items || response.Limit != test.items {
				t.Fatalf(
					"cart totals inherited pagination, foreign rows, receipt quantities or an incorrect scope: %#v",
					result,
				)
			}
			for _, item := range response.Items {
				if item["user_id"] != test.owner || item["kind"] != constants.CartRecordKindCartItem {
					t.Fatal("cart row was outside the counter scope", item)
				}
			}
		})
	}
	if _, err := module.Operations[constants.ActionList](ctx, platform.Request{}); status.Code(
		err,
	) != codes.Unauthenticated {
		t.Fatal("cart counts were available without a principal", err)
	}
	// Narrower repository reads and counters use the same optional status filter;
	// consumed cart-item rows must not contaminate an active-only caller.
	repo := repository.New(db)
	for _, fixture := range []struct {
		state           string
		total, quantity int64
	}{
		{constants.CartStatusActive, 123, 666},
		{constants.CartStatusConsumed, 2, 14},
	} {
		total, quantity, err := repo.Counts(ctx, "buyer", constants.CartRecordKindCartItem, fixture.state)
		if err != nil || total != fixture.total || quantity != fixture.quantity {
			t.Fatal("SQL counts did not preserve the optional status scope", total, quantity, err)
		}
		page, err := repo.List(ctx, "buyer", constants.CartRecordKindCartItem, fixture.state, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range page {
			if row.Status != fixture.state {
				t.Fatal("list and counters used different status scopes", row)
			}
		}
	}
}
