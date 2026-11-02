package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"net/http"
	"net/http/httptest"
	"testing"
)

type carrierOrderPeer struct{}

func (carrierOrderPeer) Call(context.Context, string, platform.Command) (platform.Result, error) {
	return platform.Result{
		"address": map[string]any{"recipient": "Buyer", "phone": "13800138000", "address": "Test address"},
		"items":   []map[string]any{{"sku_id": "coffee", "quantity": 1}},
	}, nil
}
func TestCarrierAdapterUsesStableIdempotencyAndServerAddress(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-carrier-token" ||
			r.Header.Get("Idempotency-Key") != "fulfillment-test" {
			t.Error("missing adapter authentication/idempotency")
		}
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			t.Error(e)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tracking_id":"CARRIER123"}`))
	}))
	defer server.Close()
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("LOGISTICS_CARRIER_CREATE_URL", server.URL)
	t.Setenv("LOGISTICS_CARRIER_TOKEN", "test-carrier-token")
	t.Setenv("LOGISTICS_CARRIER_PROVIDER", "test-carrier")
	s := &Service{RPC: carrierOrderPeer{}}
	id, provider, e := s.createCarrier(
		context.Background(),
		platform.Request{
			ID:     "fulfillment-test",
			UserID: "buyer",
			Role:   "system",
			Data:   map[string]any{"kind": "ORDER"},
		},
		"order-test",
	)
	if e != nil || id != "CARRIER123" || provider != "test-carrier" || payload["address"] == nil ||
		payload["items"] == nil {
		t.Fatal(id, provider, payload, e)
	}
}
