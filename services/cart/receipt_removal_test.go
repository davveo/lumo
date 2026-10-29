package cart_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	cartmod "github.com/davveo/lumo/services/cart"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type receiptPeers struct{}

func (receiptPeers) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	switch service {
	case constants.ServiceCatalog:
		return platform.Result{"id": c.ID}, nil
	case constants.ServiceInventory:
		return platform.Result{"available": 20}, nil
	case constants.ServiceOrder:
		return platform.Result{
			"user_id": c.UserID, "source": constants.OrderSourceCart,
			"items": []platform.Result{{"sku_id": "sku-receipt", "quantity": 1}},
		}, nil
	}
	return nil, status.Error(codes.NotFound, "unknown peer")
}

func TestCartRemoveCannotDeletePaymentConsumptionReceipt(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "cart.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { _ = sql.Close() })
	m := cartmod.New(platform.Dependencies{DB: db, RPC: receiptPeers{}})
	if err = m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	call := func(action, id string, data map[string]any) (platform.Result, error) {
		return m.Operations[action](
			ctx,
			platform.Request{ID: id, UserID: "buyer", Role: constants.RoleUser, Data: data},
		)
	}
	if _, err = call(constants.ActionAdd, "", map[string]any{"sku_id": "sku-receipt", "quantity": 2}); err != nil {
		t.Fatal(err)
	}
	event := platform.Event{
		ID:          "delivery-one",
		Type:        constants.EventOrderPaymentAccepted,
		AggregateID: "order-paid",
		UserID:      "buyer",
	}
	if err = m.OnEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	// The shopper knows both identities and can compute the receipt's stable ID.
	sum := sha256.Sum256([]byte("buyer\x00paid-order\x00order-paid"))
	receiptID := hex.EncodeToString(sum[:])
	if _, err = call(constants.ActionRemove, receiptID, nil); status.Code(err) != codes.NotFound {
		t.Fatalf("public removal accepted a payment receipt: %v", err)
	}
	if _, err = call(constants.ActionAdd, "", map[string]any{"sku_id": "sku-receipt", "quantity": 1}); err != nil {
		t.Fatal(err)
	}
	// At-least-once delivery must leave the newly added quantity untouched.
	if err = m.OnEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	list, err := call(constants.ActionList, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Items []platform.Result `json:"items"`
	}
	if err = platform.Decode(list, &data); err != nil || len(data.Items) != 1 ||
		platform.Int(data.Items[0], "quantity") != 2 {
		t.Fatalf("receipt deletion/replay consumed new items: %+v %v", list, err)
	}
	// Raw IDs for actual cart lines and the legacy SKU form remain supported.
	if _, err = call(constants.ActionRemove, platform.Text(data.Items[0], "id"), nil); err != nil {
		t.Fatalf("legitimate raw cart-line removal rejected: %v", err)
	}
	if _, err = call(constants.ActionAdd, "", map[string]any{"sku_id": "sku-receipt", "quantity": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = call(constants.ActionRemove, "sku-receipt", nil); err != nil {
		t.Fatalf("legacy SKU removal rejected: %v", err)
	}
}
