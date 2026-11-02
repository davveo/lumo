package tests

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTrackingSignedCallbacksIgnoreReplayAndOutOfOrderEvents(t *testing.T) {
	key := strings.Repeat("k", 32)
	t.Setenv("LOGISTICS_CARRIER_CALLBACK_KEY", key)
	t.Setenv("LOGISTICS_CARRIER_CREATE_URL", "")
	c := newCluster(t)
	ctx := context.Background()
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	create := func(id string) platform.Result {
		v, e := c.rpc.Call(
			ctx,
			"logistics",
			platform.Command{
				Action: "create",
				ID:     id,
				UserID: "demo",
				Role:   "system",
				Data:   map[string]any{"order_id": "order-tracking"},
			},
		)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	waybill := create("shipment-tracking-1")
	trackingID := platform.Text(waybill, "tracking_id")
	callback := func(event, state string, at time.Time, valid bool) int {
		body, _ := json.Marshal(
			map[string]any{
				"event_id":    event,
				"tracking_id": trackingID,
				"provider":    "local-demo-carrier",
				"status":      state,
				"occurred_at": at.UTC().Format(time.RFC3339Nano),
				"description": "Carrier scan",
			},
		)
		stamp := fmt.Sprint(time.Now().Unix())
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(stamp + "."))
		mac.Write(body)
		signature := hex.EncodeToString(mac.Sum(nil))
		if !valid {
			signature = strings.Repeat("0", 64)
		}
		req, _ := http.NewRequest(
			http.MethodPost,
			server.URL+"/api/v1/logistics/carrier_callback",
			bytes.NewReader(body),
		)
		req.Header.Set("X-Lumo-Timestamp", stamp)
		req.Header.Set("X-Lumo-Signature", signature)
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		return res.StatusCode
	}
	now := time.Now().UTC()
	if code := callback("untrusted", "PICKED_UP", now.Add(-2*time.Minute), false); code != 401 {
		t.Fatal(code)
	}
	for _, event := range []struct {
		id, state string
		at        time.Time
	}{{"pickup", "PICKED_UP", now.Add(-2 * time.Minute)}, {"transit", "IN_TRANSIT", now.Add(-time.Minute)}, {"older-exception", "EXCEPTION", now.Add(-3 * time.Minute)}, {"delivered", "DELIVERED", now}, {"delivered", "DELIVERED", now}, {"late-exception", "EXCEPTION", now.Add(time.Second)}} {
		if code := callback(event.id, event.state, event.at, true); code != 200 {
			t.Fatal(event, code)
		}
	}
	row, e := c.rpc.Call(
		ctx,
		"logistics",
		platform.Command{Action: "get", ID: "shipment-tracking-1", UserID: "demo", Role: "user"},
	)
	if e != nil || platform.Text(row, "status") != "DELIVERED" {
		t.Fatal(row, e)
	}
	_, e = c.rpc.Call(
		ctx,
		"logistics",
		platform.Command{Action: "tracking", ID: "shipment-tracking-1", UserID: "victim", Role: "user"},
	)
	if status.Code(e) != codes.NotFound {
		t.Fatal("ownership bypass", e)
	}
	create("shipment-tracking-2")
	if e = c.dbs["logistics"].Table("records").Where("id = ?", "shipment-tracking-2").Update("created_at", time.Now().Add(-48*time.Hour)).Error; e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = c.modules["logistics"].Tick(ctx); e != nil {
			t.Fatal(e)
		}
	}
	result, e := c.rpc.Call(ctx, "logistics", platform.Command{Action: "exceptions", UserID: "admin", Role: "admin"})
	if e != nil || platform.Int(result, "total") != 1 {
		t.Fatal(result, e)
	}
}
