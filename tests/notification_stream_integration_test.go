package tests

import (
	"bufio"
	"context"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNotificationStreamIsPrivateAndStopsAfterLogout(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	token := platform.Text(
		h.call("", "iam", "login", "", "", map[string]any{"username": "demo", "password": "Lumo@2026"}),
		"token",
	)
	if e := c.modules["notification"].OnEvent(context.Background(), platform.Event{
		ID:          "private-victim-notification",
		Type:        "OrderAwaitingPayment",
		AggregateID: "victim-order",
		UserID:      "victim",
		Data:        map[string]any{"order_id": "victim-order"},
	}); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/notification/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal(res.Status)
	}
	scanner := bufio.NewScanner(res.Body)
	seenData := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			if strings.Contains(line, "victim-order") {
				t.Fatal("another account leaked")
			}
			seenData = true
			break
		}
	}
	if !seenData {
		t.Fatal("no initial SSE data", scanner.Err())
	}
	h.call(token, "iam", "logout", "", "", nil)
	revoked := false
	for scanner.Scan() {
		if scanner.Text() == "event: auth_required" {
			revoked = true
			break
		}
	}
	if !revoked {
		t.Fatal("revoked stream remained usable", scanner.Err())
	}
}
