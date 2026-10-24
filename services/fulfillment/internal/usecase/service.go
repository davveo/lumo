package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
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

func (s *Service) Topics() []string { return []string{constants.EventOrderReadyToFulfill} }
func internal(c platform.Request) bool {
	return c.Role == constants.RoleSystem || c.Role == constants.RoleAdmin
}
func owned(c platform.Request, r *domain.Record) error {
	if !internal(c) && (c.UserID == "" || c.UserID != r.UserID) {
		return status.Error(codes.PermissionDenied, "fulfillment ownership mismatch")
	}
	return nil
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) (traceErr error) {
	if e.Type != constants.EventOrderReadyToFulfill {
		return nil
	}
	ctx, span := platform.Start(ctx, "usecase.fulfillment.OrderReadyToFulfill")
	defer span.End()
	platform.TraceRequest(ctx, e)
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
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
	return s.Repo.Create(
		ctx,
		&domain.Record{
			ID:            id,
			OrderID:       id,
			UserID:        e.UserID,
			ReservationID: reservation,
			Kind:          constants.FulfillmentKindOrder,
			Status:        constants.FulfillmentStatusReady,
			LinesJSON:     string(lines),
		},
	)
}
func (s *Service) Cancel(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.fulfillment."+constants.ActionCancel)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	if !internal(c) {
		return nil, status.Error(codes.PermissionDenied, "internal cancellation command required")
	}
	r, err := s.Repo.Get(ctx, c.ID)
	if status.Code(err) == codes.NotFound {

		if c.ID == "" || c.UserID == "" {
			return nil, status.Error(codes.InvalidArgument, "cancellation requires order and owner")
		}
		user := c.UserID
		if c.Role == constants.RoleAdmin {
			order, callErr := s.RPC.Call(
				ctx,
				constants.ServiceOrder,
				platform.Command{Action: constants.ActionGet, ID: c.ID, UserID: c.UserID, Role: c.Role},
			)
			if callErr != nil {
				return nil, callErr
			}
			user = platform.Text(order, "user_id")
			if user == "" {
				return nil, status.Error(codes.FailedPrecondition, "order owner unavailable")
			}
		}
		r = &domain.Record{
			ID:            c.ID,
			OrderID:       c.ID,
			UserID:        user,
			ReservationID: c.ID,
			Kind:          constants.FulfillmentKindOrder,
			Status:        constants.FulfillmentStatusCancelRequested,
		}
		if err = s.Repo.Create(ctx, r); err != nil {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	if r.Status == constants.FulfillmentStatusCancelled {
		out := platform.Map(r)
		out["warehouse_confirmed"] = true
		return out, nil
	}
	if r.Status == constants.FulfillmentStatusDispatched || r.Status == constants.FulfillmentStatusDispatching {
		return nil, status.Error(codes.FailedPrecondition, "warehouse shipment cannot be cancelled")
	}
	if r.Status == constants.FulfillmentStatusReady {
		r, err = s.Repo.Transition(
			ctx,
			r.ID,
			[]string{constants.FulfillmentStatusReady},
			constants.FulfillmentStatusCancelRequested,
			nil,
			nil,
		)
		if err != nil {
			return nil, err
		}
	}
	if r.Status == constants.FulfillmentStatusCancelRequested {
		if os.Getenv("DEMO_MODE") != "true" {
			return nil, status.Error(
				codes.FailedPrecondition,
				"awaiting authenticated warehouse cancellation acknowledgement",
			)
		}

		r, err = s.Repo.Transition(
			ctx,
			r.ID,
			[]string{constants.FulfillmentStatusCancelRequested},
			constants.FulfillmentStatusWarehouseCancelConfirmed,
			nil,
			nil,
		)
		if err != nil {
			return nil, err
		}
	}
	if _, err = s.RPC.Call(
		ctx,
		constants.ServiceInventory,
		platform.Command{
			Action: constants.ActionCancel,
			ID:     r.ReservationID,
			UserID: r.UserID,
			Role:   constants.RoleSystem,
			Data:   map[string]any{"warehouse_confirmed": true},
		},
	); err != nil {
		return nil, err
	}
	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.FulfillmentStatusWarehouseCancelConfirmed},
		constants.FulfillmentStatusCancelled,
		nil,
		nil,
	)
	out := platform.Map(r)
	out["warehouse_confirmed"] = true
	return out, err

}

func (s *Service) CreateExchange(
	ctx context.Context,
	c platform.Request,
) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.fulfillment."+constants.ActionCreateExchange)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	if !internal(c) {
		return nil, status.Error(codes.PermissionDenied, "internal exchange command required")
	}
	orderID := platform.Text(c.Data, "order_id")
	reservation := platform.Text(c.Data, "reservation_id")
	if c.ID == "" || c.UserID == "" || orderID == "" || reservation == "" {
		return nil, status.Error(codes.InvalidArgument, "exchange identity, order and reservation required")
	}
	lines, _ := json.Marshal(c.Data["lines"])
	r := &domain.Record{
		ID:            c.ID,
		OrderID:       orderID,
		UserID:        c.UserID,
		ReservationID: reservation,
		Kind:          constants.FulfillmentKindExchange,
		Status:        constants.FulfillmentStatusReady,
		LinesJSON:     string(lines),
	}
	if err := s.Repo.Create(ctx, r); err != nil {
		return nil, err
	}
	return platform.Map(r), nil

}

func (s *Service) Dispatch(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.fulfillment."+constants.ActionDispatch)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	if !internal(c) {
		return nil, status.Error(codes.PermissionDenied, "warehouse permission required")
	}
	if os.Getenv("DEMO_MODE") != "true" {
		return nil, status.Error(
			codes.FailedPrecondition,
			"real warehouse adapter is not configured; demo dispatch requires DEMO_MODE=true",
		)
	}
	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if r.Status == constants.FulfillmentStatusDispatched {
		return platform.Map(r), nil
	}
	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.FulfillmentStatusReady},
		constants.FulfillmentStatusDispatching,
		nil,
		nil,
	)
	if err != nil {
		return nil, err
	}
	command := platform.Command{
		ID:     r.ReservationID,
		UserID: r.UserID,
		Role:   constants.RoleSystem,
		Action: constants.ActionDispatch,
		Data:   map[string]any{"warehouse_confirmed": true},
	}
	if _, err = s.RPC.Call(ctx, constants.ServiceInventory, command); err != nil {
		return nil, err
	}
	shipping, err := s.RPC.Call(
		ctx,
		constants.ServiceLogistics,
		platform.Command{
			Action: constants.ActionCreate,
			ID:     r.ID,
			UserID: r.UserID,
			Role:   constants.RoleSystem,
			Data:   map[string]any{"order_id": r.OrderID, "kind": r.Kind},
		},
	)
	if err != nil {
		return nil, err
	}
	tracking := platform.Text(shipping, "tracking_id")
	event := &platform.Event{
		ID:          r.ID + ":dispatched",
		Type:        constants.EventShipmentDispatched,
		AggregateID: r.ID,
		UserID:      r.UserID,
		Data: map[string]any{
			"order_id":       r.OrderID,
			"fulfillment_id": r.ID,
			"tracking_id":    tracking,
			"kind":           r.Kind,
		},
	}
	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.FulfillmentStatusDispatching},
		constants.FulfillmentStatusDispatched,
		map[string]any{"tracking_id": tracking},
		event,
	)
	return platform.Map(r), err

}

func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.fulfillment."+constants.ActionGet)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owned(c, r); err != nil {
		return nil, err
	}
	return platform.Map(r), nil

}

func (s *Service) InspectReturn(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.fulfillment."+constants.ActionInspectReturn)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	if !internal(c) {
		return nil, status.Error(codes.PermissionDenied, "warehouse inspection permission required")
	}
	condition := strings.ToLower(platform.Text(c.Data, "condition"))
	sku := platform.Text(c.Data, "sku_id")
	qty := platform.Int(c.Data, "quantity")
	if c.ID == "" || sku == "" || qty <= 0 ||
		(condition != constants.StockConditionSellable && condition != constants.StockConditionDamaged &&
			condition != constants.StockConditionUnsellable) {
		return nil, status.Error(
			codes.InvalidArgument,
			"valid return receipt, SKU, quantity and condition required",
		)
	}
	payload, _ := json.Marshal(map[string]any{"sku_id": sku, "quantity": qty, "condition": condition})
	receipt := &domain.Record{
		ID:            "return:" + c.ID,
		OrderID:       platform.Text(c.Data, "order_id"),
		UserID:        c.UserID,
		ReservationID: c.ID,
		Kind:          constants.FulfillmentKindReturn,
		Status:        constants.FulfillmentStatusInspecting,
		LinesJSON:     string(payload),
	}
	if err := s.Repo.Create(ctx, receipt); err != nil {
		return nil, err
	}
	if receipt.LinesJSON != string(payload) {
		return nil, status.Error(codes.AlreadyExists, "inspection id reused with a different receipt")
	}
	if receipt.Status == constants.FulfillmentStatusInspected {
		return platform.Map(receipt), nil
	}
	if _, err := s.RPC.Call(
		ctx,
		constants.ServiceInventory,
		platform.Command{
			Action: constants.ActionRestock,
			ID:     c.ID,
			UserID: c.UserID,
			Role:   constants.RoleSystem,
			Data:   map[string]any{"sku_id": sku, "quantity": qty, "condition": condition},
		},
	); err != nil {
		return nil, err
	}
	event := &platform.Event{
		ID:          receipt.ID + ":inspected",
		Type:        constants.EventReturnInspectionCompleted,
		AggregateID: receipt.ID,
		UserID:      receipt.UserID,
		Data:        map[string]any{"inspection_id": c.ID, "sku_id": sku, "quantity": qty, "condition": condition},
	}
	receipt, err := s.Repo.Transition(
		ctx,
		receipt.ID,
		[]string{constants.FulfillmentStatusInspecting},
		constants.FulfillmentStatusInspected,
		nil,
		event,
	)
	return platform.Map(receipt), err

}

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.fulfillment."+constants.ActionList)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	user := c.UserID
	if internal(c) {
		user = ""
	}
	rows, err := s.Repo.List(ctx, user)
	return platform.Result{"items": rows}, err

}
