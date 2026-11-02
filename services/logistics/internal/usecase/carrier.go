package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// The configured adapter speaks the documented LUMO carrier webhook contract.
func (s *Service) createCarrier(ctx context.Context, c platform.Request, orderID string) (string, string, error) {
	endpoint, credential, provider := os.Getenv(
		"LOGISTICS_CARRIER_CREATE_URL",
	), os.Getenv(
		"LOGISTICS_CARRIER_TOKEN",
	), os.Getenv(
		"LOGISTICS_CARRIER_PROVIDER",
	)
	u, e := url.Parse(endpoint)
	local := e == nil && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1") &&
		os.Getenv("DEMO_MODE") == "true"
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" ||
		u.Scheme != "https" && !(local && u.Scheme == "http") ||
		credential == "" ||
		provider == "" ||
		len(provider) > 40 {
		return "", "", status.Error(codes.FailedPrecondition, "物流渠道配置不完整")
	}
	order, e := platform.NewClients(s.RPC).Order.Get(ctx, platform.Request{ID: orderID, UserID: c.UserID, Role: c.Role})
	if e != nil {
		return "", "", e
	}
	body, _ := json.Marshal(
		map[string]any{
			"fulfillment_id": c.ID,
			"order_id":       orderID,
			"kind":           c.Data["kind"],
			"address":        order["address"],
			"items":          order["items"],
		},
	)
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if e != nil {
		return "", "", e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Idempotency-Key", c.ID)
	client := &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, e := client.Do(req)
	if e != nil {
		return "", "", status.Error(codes.Unavailable, "物流渠道暂不可用，请稍后重试")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", status.Error(codes.Unavailable, "物流渠道未受理运单")
	}
	var out struct {
		TrackingID string `json:"tracking_id"`
	}
	if e = json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&out); e != nil || out.TrackingID == "" ||
		len(out.TrackingID) > 100 ||
		strings.TrimSpace(out.TrackingID) != out.TrackingID {
		return "", "", status.Error(codes.Unavailable, "物流渠道返回无效运单号")
	}
	return out.TrackingID, provider, nil
}
