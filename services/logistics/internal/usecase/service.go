package usecase

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/logistics/internal/domain"
	"github.com/davveo/lumo/services/logistics/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
)

type Service struct {
	Repo *repository.Repository
	RPC  platform.Caller
}

func (s *Service) Topics() []string                              { return nil }
func (s *Service) OnEvent(context.Context, platform.Event) error { return nil }
func privileged(c platform.Command) bool                         { return c.Role == "admin" || c.Role == "system" }
func (s *Service) Handle(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.logistics."+c.Action)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	switch c.Action {
	case "quote":
		return platform.Result{"shipping_cents": int64(0), "provider": "demo-free-shipping", "serviceable": true}, nil
	case "list":
		if err := platform.RequireUser(c); err != nil {
			return nil, err
		}
		user := c.UserID
		if privileged(c) {
			user = ""
		}
		rows, err := s.Repo.List(ctx, user)
		return platform.Result{"items": rows}, err
	case "create":
		if !privileged(c) {
			return nil, status.Error(codes.PermissionDenied, "internal fulfillment permission required")
		}
		if os.Getenv("DEMO_MODE") != "true" {
			return nil, status.Error(codes.FailedPrecondition, "real carrier adapter not configured; local carrier requires DEMO_MODE=true")
		}
		orderID := platform.Text(c.Data, "order_id")
		if orderID == "" {
			orderID = c.ID
		}
		if c.ID == "" || c.UserID == "" {
			return nil, status.Error(codes.InvalidArgument, "fulfillment id and owner required")
		}
		kind := platform.Text(c.Data, "kind")
		if kind == "" {
			kind = "ORDER"
		}
		hash := sha256.Sum256([]byte(c.ID))
		r := &domain.Record{ID: c.ID, OrderID: orderID, UserID: c.UserID, Kind: kind, Status: "CREATED", TrackingID: fmt.Sprintf("DEMO-%X", hash[:10]), Provider: "local-demo-carrier"}
		if err := s.Repo.Create(ctx, r); err != nil {
			return nil, err
		}
		return platform.Map(r), nil
	case "get":
		r, err := s.Repo.Get(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		if !privileged(c) && (c.UserID == "" || r.UserID != c.UserID) {
			return nil, status.Error(codes.PermissionDenied, "waybill ownership mismatch")
		}
		return platform.Map(r), nil
	case "deliver":
		if !privileged(c) {
			return nil, status.Error(codes.PermissionDenied, "carrier callback/admin permission required")
		}
		r, err := s.Repo.Get(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		event := &platform.Event{ID: r.ID + ":delivered", Type: "DeliveryConfirmed", AggregateID: r.ID, UserID: r.UserID, Data: map[string]any{"order_id": r.OrderID, "fulfillment_id": r.ID, "tracking_id": r.TrackingID, "kind": r.Kind, "provider": r.Provider}}
		r, err = s.Repo.Transition(ctx, r.ID, []string{"CREATED", "IN_TRANSIT"}, "DELIVERED", nil, event)
		return platform.Map(r), err
	}
	return nil, status.Error(codes.Unimplemented, "unsupported logistics action")
}
