package usecase

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/fulfillment/internal/domain"
	"github.com/davveo/lumo/services/fulfillment/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service coordinates a demo warehouse adapter. The two cancellation phases are
// persisted; a real WMS adapter must replace the explicit demo acknowledgement.
type Service struct {
	Repo *repository.Repository
	RPC  platform.Caller
}

func (s *Service) Topics() []string    { return []string{"OrderReadyToFulfill"} }
func internal(c platform.Command) bool { return c.Role == "system" || c.Role == "admin" }
func owned(c platform.Command, r *domain.Record) error {
	if !internal(c) && (c.UserID == "" || c.UserID != r.UserID) {
		return status.Error(codes.PermissionDenied, "fulfillment ownership mismatch")
	}
	return nil
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type != "OrderReadyToFulfill" {
		return nil
	}
	ctx, span := platform.Start(ctx, "usecase.fulfillment.OrderReadyToFulfill")
	defer span.End()
	id := platform.Text(e.Data, "order_id")
	if id == "" {
		id = e.AggregateID
	}
	reservation := platform.Text(e.Data, "reservation_id")
	if reservation == "" {
		reservation = id
	}
	if id == "" || e.UserID == "" {
		return status.Error(codes.InvalidArgument, "order event lacks identity")
	}
	lines, _ := json.Marshal(e.Data["items"])
	return s.Repo.Create(ctx, &domain.Record{ID: id, OrderID: id, UserID: e.UserID, ReservationID: reservation, Kind: "ORDER", Status: "READY", LinesJSON: string(lines)})
}
func (s *Service) Handle(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "usecase.fulfillment."+c.Action)
	defer span.End()
	switch c.Action {
	case "list":
		if err := platform.RequireUser(c); err != nil {
			return nil, err
		}
		user := c.UserID
		if internal(c) {
			user = ""
		}
		rows, err := s.Repo.List(ctx, user)
		return platform.Result{"items": rows}, err
	case "get":
		r, err := s.Repo.Get(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		if err = owned(c, r); err != nil {
			return nil, err
		}
		return platform.Map(r), nil
	case "create_exchange":
		if !internal(c) {
			return nil, status.Error(codes.PermissionDenied, "internal exchange command required")
		}
		orderID := platform.Text(c.Data, "order_id")
		reservation := platform.Text(c.Data, "reservation_id")
		if c.ID == "" || c.UserID == "" || orderID == "" || reservation == "" {
			return nil, status.Error(codes.InvalidArgument, "exchange identity, order and reservation required")
		}
		lines, _ := json.Marshal(c.Data["lines"])
		r := &domain.Record{ID: c.ID, OrderID: orderID, UserID: c.UserID, ReservationID: reservation, Kind: "EXCHANGE", Status: "READY", LinesJSON: string(lines)}
		if err := s.Repo.Create(ctx, r); err != nil {
			return nil, err
		}
		return platform.Map(r), nil
	case "dispatch":
		if !internal(c) {
			return nil, status.Error(codes.PermissionDenied, "warehouse permission required")
		}
		if os.Getenv("DEMO_MODE") != "true" {
			return nil, status.Error(codes.FailedPrecondition, "real warehouse adapter is not configured; demo dispatch requires DEMO_MODE=true")
		}
		r, err := s.Repo.Get(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		if r.Status == "DISPATCHED" {
			return platform.Map(r), nil
		}
		r, err = s.Repo.Transition(ctx, r.ID, []string{"READY"}, "DISPATCHING", nil, nil)
		if err != nil {
			return nil, err
		}
		command := platform.Command{ID: r.ReservationID, UserID: r.UserID, Role: "system", Action: "dispatch", Data: map[string]any{"warehouse_confirmed": true}}
		if _, err = s.RPC.Call(ctx, "inventory", command); err != nil {
			return nil, err
		}
		shipping, err := s.RPC.Call(ctx, "logistics", platform.Command{Action: "create", ID: r.ID, UserID: r.UserID, Role: "system", Data: map[string]any{"order_id": r.OrderID, "kind": r.Kind}})
		if err != nil {
			return nil, err
		}
		tracking := platform.Text(shipping, "tracking_id")
		event := &platform.Event{ID: r.ID + ":dispatched", Type: "ShipmentDispatched", AggregateID: r.ID, UserID: r.UserID, Data: map[string]any{"order_id": r.OrderID, "fulfillment_id": r.ID, "tracking_id": tracking, "kind": r.Kind}}
		r, err = s.Repo.Transition(ctx, r.ID, []string{"DISPATCHING"}, "DISPATCHED", map[string]any{"tracking_id": tracking}, event)
		return platform.Map(r), err
	case "cancel":
		if !internal(c) {
			return nil, status.Error(codes.PermissionDenied, "internal cancellation command required")
		}
		r, err := s.Repo.Get(ctx, c.ID)
		if status.Code(err) == codes.NotFound {
			// A cancellation may overtake the asynchronous ready event. Persist a
			// tombstone first so that the delayed event cannot resurrect shipment.
			if c.ID == "" || c.UserID == "" {
				return nil, status.Error(codes.InvalidArgument, "cancellation requires order and owner")
			}
			user := c.UserID
			if c.Role == "admin" {
				order, callErr := s.RPC.Call(ctx, "order", platform.Command{Action: "get", ID: c.ID, UserID: c.UserID, Role: c.Role})
				if callErr != nil {
					return nil, callErr
				}
				user = platform.Text(order, "user_id")
				if user == "" {
					return nil, status.Error(codes.FailedPrecondition, "order owner unavailable")
				}
			}
			r = &domain.Record{ID: c.ID, OrderID: c.ID, UserID: user, ReservationID: c.ID, Kind: "ORDER", Status: "CANCEL_REQUESTED"}
			if err = s.Repo.Create(ctx, r); err != nil {
				return nil, err
			}
		}
		if err != nil {
			return nil, err
		}
		if r.Status == "CANCELLED" {
			out := platform.Map(r)
			out["warehouse_confirmed"] = true
			return out, nil
		}
		if r.Status == "DISPATCHED" || r.Status == "DISPATCHING" {
			return nil, status.Error(codes.FailedPrecondition, "warehouse shipment cannot be cancelled")
		}
		if r.Status == "READY" {
			r, err = s.Repo.Transition(ctx, r.ID, []string{"READY"}, "CANCEL_REQUESTED", nil, nil)
			if err != nil {
				return nil, err
			}
		}
		if r.Status == "CANCEL_REQUESTED" {
			if os.Getenv("DEMO_MODE") != "true" {
				return nil, status.Error(codes.FailedPrecondition, "awaiting authenticated warehouse cancellation acknowledgement")
			}
			// This acknowledgement is ONLY the local deterministic demo warehouse.
			// The production adapter must wait for an authenticated WMS callback.
			r, err = s.Repo.Transition(ctx, r.ID, []string{"CANCEL_REQUESTED"}, "WAREHOUSE_CANCEL_CONFIRMED", nil, nil)
			if err != nil {
				return nil, err
			}
		}
		if _, err = s.RPC.Call(ctx, "inventory", platform.Command{Action: "cancel", ID: r.ReservationID, UserID: r.UserID, Role: "system", Data: map[string]any{"warehouse_confirmed": true}}); err != nil {
			return nil, err
		}
		r, err = s.Repo.Transition(ctx, r.ID, []string{"WAREHOUSE_CANCEL_CONFIRMED"}, "CANCELLED", nil, nil)
		out := platform.Map(r)
		out["warehouse_confirmed"] = true
		return out, err
	case "inspect_return":
		if !internal(c) {
			return nil, status.Error(codes.PermissionDenied, "warehouse inspection permission required")
		}
		condition := strings.ToLower(platform.Text(c.Data, "condition"))
		sku := platform.Text(c.Data, "sku_id")
		qty := platform.Int(c.Data, "quantity")
		if c.ID == "" || sku == "" || qty <= 0 || (condition != "sellable" && condition != "damaged" && condition != "unsellable") {
			return nil, status.Error(codes.InvalidArgument, "valid return receipt, SKU, quantity and condition required")
		}
		payload, _ := json.Marshal(map[string]any{"sku_id": sku, "quantity": qty, "condition": condition})
		receipt := &domain.Record{ID: "return:" + c.ID, OrderID: platform.Text(c.Data, "order_id"), UserID: c.UserID, ReservationID: c.ID, Kind: "RETURN", Status: "INSPECTING", LinesJSON: string(payload)}
		if err := s.Repo.Create(ctx, receipt); err != nil {
			return nil, err
		}
		if receipt.LinesJSON != string(payload) {
			return nil, status.Error(codes.AlreadyExists, "inspection id reused with a different receipt")
		}
		if receipt.Status == "INSPECTED" {
			return platform.Map(receipt), nil
		}
		if _, err := s.RPC.Call(ctx, "inventory", platform.Command{Action: "restock", ID: c.ID, UserID: c.UserID, Role: "system", Data: map[string]any{"sku_id": sku, "quantity": qty, "condition": condition}}); err != nil {
			return nil, err
		}
		event := &platform.Event{ID: receipt.ID + ":inspected", Type: "ReturnInspectionCompleted", AggregateID: receipt.ID, UserID: receipt.UserID, Data: map[string]any{"inspection_id": c.ID, "sku_id": sku, "quantity": qty, "condition": condition}}
		receipt, err := s.Repo.Transition(ctx, receipt.ID, []string{"INSPECTING"}, "INSPECTED", nil, event)
		return platform.Map(receipt), err
	}
	return nil, status.Error(codes.Unimplemented, "unsupported fulfillment action")
}
