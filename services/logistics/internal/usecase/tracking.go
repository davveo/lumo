package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/logistics/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
	"strconv"
	"strings"
	"time"
)

func (s *Service) Tracking(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.ID == "" || len(c.ID) > 128 {
		return nil, status.Error(codes.InvalidArgument, "请提供订单或运单编号")
	}
	return s.Repo.Tracking(ctx, c)
}
func (s *Service) TrackingUpdate(ctx context.Context, c platform.Request) (platform.Result, error) {
	if !privileged(c) {
		return nil, status.Error(codes.PermissionDenied, "carrier or warehouse permission required")
	}
	var in struct {
		TrackingID  string `json:"tracking_id"`
		Provider    string `json:"provider"`
		Status      string `json:"status"`
		Location    string `json:"location"`
		Description string `json:"description"`
		Occurred    string `json:"occurred_at"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	valid := map[string]bool{
		constants.TrackingStatusCreated:        true,
		constants.TrackingStatusPickedUp:       true,
		constants.TrackingStatusInTransit:      true,
		constants.TrackingStatusOutForDelivery: true,
		constants.TrackingStatusDelivered:      true,
		constants.TrackingStatusException:      true,
	}
	at, e := time.Parse(time.RFC3339Nano, in.Occurred)
	if c.ID == "" || len(c.ID) > 128 || in.TrackingID == "" || len(in.TrackingID) > 100 || len(in.Provider) > 40 ||
		!valid[in.Status] ||
		e != nil ||
		at.After(time.Now().Add(5*time.Minute)) ||
		len(in.Location) > 160 ||
		len(in.Description) > 500 {
		return nil, status.Error(codes.InvalidArgument, "物流事件的编号、时间、状态或内容无效")
	}
	if platform.Text(c.Data, "_source") == "carrier" && in.Provider == "" {
		return nil, status.Error(codes.InvalidArgument, "物流回调需包含渠道编号")
	}
	raw, _ := json.Marshal(in)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(raw))
	id := fmt.Sprintf("TRK_%x", sha256.Sum256([]byte(in.Provider+"\x00"+c.ID)))
	source := "staff"
	if c.Role == "system" && platform.Text(c.Data, "_source") == "carrier" {
		source = "carrier"
	}
	if platform.Text(c.Data, "_source") == "warehouse" {
		source = "warehouse"
	}
	return s.Repo.TrackUpdate(
		ctx,
		c,
		domain.TrackingEvent{
			ID:          id,
			TrackingID:  in.TrackingID,
			Status:      in.Status,
			Location:    in.Location,
			Description: in.Description,
			Source:      source,
			ActorID:     c.UserID,
			Fingerprint: fingerprint,
			OccurredAt:  at.UTC(),
		},
		in.Provider,
	)
}
func (s *Service) Exceptions(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.Exceptions(ctx, c)
}
func (s *Service) ExceptionResolve(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	resolution := strings.TrimSpace(platform.Text(c.Data, "resolution"))
	if resolution == "" || len(resolution) > 500 {
		return nil, status.Error(codes.InvalidArgument, "请填写异常处理结果，最多500字节")
	}
	return s.Repo.ResolveException(ctx, c)
}
func trackingHours(key string, fallback int) time.Duration {
	n, e := strconv.Atoi(os.Getenv(key))
	if e != nil || n < 1 || n > 720 {
		n = fallback
	}
	return time.Duration(n) * time.Hour
}
func (s *Service) TickTracking(ctx context.Context) error {
	return s.Repo.ScanDeliveryExceptions(
		ctx,
		trackingHours("LOGISTICS_PICKUP_TIMEOUT_HOURS", 24),
		trackingHours("LOGISTICS_STALE_TIMEOUT_HOURS", 72),
	)
}
