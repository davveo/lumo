package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"time"
)

type Service struct {
	r   *repository.Repository
	rpc platform.Caller
}

func New(r *repository.Repository, rpc platform.Caller) *Service { return &Service{r, rpc} }
func (s *Service) Topics() []string {
	return []string{"PaymentSucceeded", "RefundSucceeded", "ShipmentDispatched", "DeliveryConfirmed", "GroupFormed", "GroupFailed"}
}
func own(c platform.Command, o domain.Order) error {
	if c.UserID != o.UserID && c.Role != "system" && c.Role != "admin" {
		return status.Error(codes.PermissionDenied, "order belongs to another member")
	}
	return nil
}
func internal(c platform.Command) error {
	if c.Role != "system" && c.Role != "admin" {
		return status.Error(codes.PermissionDenied, "internal order command")
	}
	return nil
}
func view(o domain.Order) platform.Result {
	out := platform.Map(o)
	var q, a map[string]any
	_ = json.Unmarshal([]byte(o.Quote), &q)
	_ = json.Unmarshal([]byte(o.Address), &a)
	out["quote"] = q
	out["address"] = a
	out["items"] = q["items"]
	out["coupon_id"] = q["coupon_id"]
	out["points"] = q["points"]
	out["reservation_id"] = o.ID
	return out
}
func (s *Service) Handle(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "order.usecase."+c.Action)
	defer sp.End()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Action == "list" {
		all := c.Role == "admin" || c.Role == "system"
		rows, err := s.r.List(ctx, c.UserID, all)
		if err != nil {
			return nil, err
		}
		items := []platform.Result{}
		for _, o := range rows {
			items = append(items, view(o))
		}
		return platform.Result{"items": items}, nil
	}
	if c.Action == "get" {
		o, err := s.r.Get(ctx, c.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "order not found")
		}
		if err != nil {
			return nil, err
		}
		if err = own(c, o); err != nil {
			return nil, err
		}
		return view(o), nil
	}
	if c.Action != "complete" {
		if err := internal(c); err != nil {
			return nil, err
		}
	}
	var result platform.Result
	err := s.r.Transaction(ctx, func(tx *repository.Tx) error {
		o, err := tx.Get(c.ID)
		if c.Action == "create" {
			if err == nil {
				if e := own(c, o); e != nil {
					return e
				}
				result = view(o)
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if c.ID == "" {
				return status.Error(codes.InvalidArgument, "order ID required")
			}
			q, ok := c.Data["quote"].(map[string]any)
			if !ok {
				return status.Error(codes.InvalidArgument, "quote snapshot required")
			}
			amount := platform.Int(q, "payable_cents")
			if amount <= 0 {
				return status.Error(codes.InvalidArgument, "payable amount required")
			}
			qb, _ := json.Marshal(q)
			ab, _ := json.Marshal(c.Data["address"])
			expiry := time.Now().UTC().Add(30 * time.Minute)
			if raw := platform.Text(c.Data, "expires_at"); raw != "" {
				if t, e := time.Parse(time.RFC3339Nano, raw); e == nil {
					expiry = t
				}
			}
			o = domain.Order{ID: c.ID, UserID: c.UserID, Status: "CREATING", Version: 1, Payable: amount, Quote: string(qb), Address: string(ab), Note: platform.Text(c.Data, "note"), Source: platform.Text(c.Data, "source"), GroupID: platform.Text(q, "group_id"), ExpiresAt: expiry}
			if err = tx.Create(&o); err != nil {
				return err
			}
			result = view(o)
			return nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return status.Error(codes.NotFound, "order not found")
		}
		if err != nil {
			return err
		}
		if err = own(c, o); err != nil {
			return err
		}
		emit := ""
		data := map[string]any{"order_id": o.ID}
		switch c.Action {
		case "pending":
			if o.Status == "PENDING_PAYMENT" {
				result = view(o)
				return nil
			}
			if o.Status != "CREATING" {
				return status.Error(codes.FailedPrecondition, "order cannot become payable")
			}
			o.Status = "PENDING_PAYMENT"
			emit = "OrderAwaitingPayment"
		case "create_failed":
			if o.Status == "CREATING" {
				o.Status = "CREATE_FAILED"
			}
		case "accept_payment":
			pid := platform.Text(c.Data, "payment_id")
			amount := platform.Int(c.Data, "amount_cents")
			if pid == "" || amount != o.Payable {
				return status.Error(codes.InvalidArgument, "payment does not match order")
			}
			accepted := false
			if o.PaymentID == pid {
				accepted = true
			} else if o.PaymentID == "" && (o.Status == "PENDING_PAYMENT" || o.Status == "CLOSING") {
				paidAt := time.Now().UTC()
				if raw := platform.Text(c.Data, "paid_at"); raw != "" {
					if t, e := time.Parse(time.RFC3339Nano, raw); e == nil {
						paidAt = t
					}
				}
				if !paidAt.After(o.ExpiresAt) {
					o.PaymentID = pid
					o.Status = "PAID_CONFIRMING"
					accepted = true
					emit = "OrderPaymentAccepted"
				}
			}
			if !accepted {
				emit = "PaymentRejected"
				if o.PaymentID == "" && (o.Status == "PENDING_PAYMENT" || o.Status == "CLOSING") {
					o.Status = "CLOSED"
					if err = tx.Emit(platform.Event{ID: "OrderClosed-" + o.ID, Type: "OrderClosed", AggregateID: o.ID, UserID: o.UserID, Data: map[string]any{"order_id": o.ID, "reason": "payment_after_deadline"}}); err != nil {
						return err
					}
				}
			}
			data["payment_id"] = pid
			data["amount_cents"] = amount
			result = platform.Result{"accepted": accepted, "order_id": o.ID, "status": o.Status, "payment_id": pid}
			if err = tx.Save(&o); err != nil {
				return err
			}
			if emit != "" {
				return tx.Emit(platform.Event{ID: emit + "-" + pid, Type: emit, AggregateID: o.ID, UserID: o.UserID, Data: data})
			}
			return nil
		case "ready":
			if o.GroupFailed {
				return status.Error(codes.FailedPrecondition, "group already failed")
			}
			if o.Status == "READY_TO_FULFILL" || o.Status == "SHIPPED" || o.Status == "COMPLETED" {
				result = view(o)
				return nil
			}
			if o.Status != "PAID_CONFIRMING" && o.Status != "PAID_WAIT_GROUP" {
				return status.Error(codes.FailedPrecondition, "payment not accepted")
			}
			o.ResourcesCommitted = true
			if o.GroupID != "" && !o.GroupFormed {
				o.Status = "PAID_WAIT_GROUP"
			} else {
				o.Status = "READY_TO_FULFILL"
				emit = "OrderReadyToFulfill"
				data["reservation_id"] = o.ID
				var q map[string]any
				_ = json.Unmarshal([]byte(o.Quote), &q)
				data["items"] = q["items"]
			}
		case "wait_group":
			if o.Status == "PAID_CONFIRMING" {
				o.ResourcesCommitted = true
				o.Status = "PAID_WAIT_GROUP"
			}
		case "close_begin":
			if o.Status == "CLOSED" || o.Status == "CREATE_FAILED" {
				result = view(o)
				return nil
			}
			if o.Status != "PENDING_PAYMENT" && o.Status != "CLOSING" {
				return status.Error(codes.FailedPrecondition, "only unpaid orders can close")
			}
			o.Status = "CLOSING"
		case "close_complete":
			if o.Status == "CLOSED" {
				result = view(o)
				return nil
			}
			if o.Status != "CLOSING" {
				return status.Error(codes.FailedPrecondition, "close was not begun")
			}
			o.Status = "CLOSED"
			emit = "OrderClosed"
		case "group_formed":
			if o.GroupFailed {
				return status.Error(codes.FailedPrecondition, "group already failed")
			}
			o.GroupFormed = true
		case "group_failed":
			if o.Status == "SHIPPED" || o.Status == "COMPLETED" {
				return status.Error(codes.FailedPrecondition, "cannot fail dispatched group order")
			}
			o.GroupFailed = true
			o.Status = "TERMINATED"
			emit = "GroupOrderTerminated"
			data["payment_id"] = o.PaymentID
			data["amount_cents"] = o.Payable
		case "shipped":
			if o.Status == "SHIPPED" || o.Status == "COMPLETED" {
				result = view(o)
				return nil
			}
			if o.Status != "READY_TO_FULFILL" {
				return status.Error(codes.FailedPrecondition, "order not ready to ship")
			}
			o.Status = "SHIPPED"
		case "complete":
			if o.Status == "COMPLETED" {
				result = view(o)
				return nil
			}
			if o.Status != "SHIPPED" {
				return status.Error(codes.FailedPrecondition, "order not shipped")
			}
			o.Status = "COMPLETED"
			emit = "OrderCompleted"
			data["payable_cents"] = o.Payable
		case "refunded":
			rid := platform.Text(c.Data, "refund_id")
			amt := platform.Int(c.Data, "amount_cents")
			if rid == "" || amt <= 0 {
				return status.Error(codes.InvalidArgument, "refund receipt required")
			}
			if _, e := tx.Receipt(rid); e == nil {
				result = view(o)
				return nil
			} else if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if o.Refunded+amt > o.Payable {
				return status.Error(codes.FailedPrecondition, "refund exceeds captured amount")
			}
			o.Refunded += amt
			if err = tx.AddReceipt(domain.RefundReceipt{ID: rid, OrderID: o.ID, Amount: amt}); err != nil {
				return err
			}
		default:
			return status.Error(codes.Unimplemented, "unsupported order action")
		}
		if err = tx.Save(&o); err != nil {
			return err
		}
		if emit != "" {
			if err = tx.Emit(platform.Event{ID: emit + "-" + o.ID, Type: emit, AggregateID: o.ID, UserID: o.UserID, Data: data}); err != nil {
				return err
			}
		}
		result = view(o)
		return nil
	})
	return result, err
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	c := platform.Command{ID: e.AggregateID, UserID: e.UserID, Role: "system", Data: e.Data}
	switch e.Type {
	case "PaymentSucceeded":
		c.Action = "accept_payment"
		c.ID = platform.Text(e.Data, "order_id")
		_, err := s.Handle(ctx, c)
		return err
	case "RefundSucceeded":
		c.Action = "refunded"
		c.ID = platform.Text(e.Data, "order_id")
		_, err := s.Handle(ctx, c)
		return err
	case "ShipmentDispatched":
		if platform.Text(e.Data, "kind") == "EXCHANGE" {
			return nil
		}
		c.Action = "shipped"
		c.ID = platform.Text(e.Data, "order_id")
		_, err := s.Handle(ctx, c)
		return err
	case "DeliveryConfirmed":
		if platform.Text(e.Data, "kind") == "EXCHANGE" {
			return nil
		}
		c.Action = "complete"
		c.ID = platform.Text(e.Data, "order_id")
		_, err := s.Handle(ctx, c)
		return err
	case "GroupFormed", "GroupFailed":
		var ids []string
		b, _ := json.Marshal(e.Data["order_ids"])
		_ = json.Unmarshal(b, &ids)
		if id := platform.Text(e.Data, "order_id"); id != "" {
			ids = append(ids, id)
		}
		for _, id := range ids {
			o, err := s.r.Get(ctx, id)
			if err != nil {
				return err
			}
			action := "group_formed"
			if e.Type == "GroupFailed" {
				action = "group_failed"
			}
			if _, err = s.Handle(ctx, platform.Command{Action: action, ID: id, UserID: o.UserID, Role: "system", Data: e.Data}); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}
func (s *Service) Tick(ctx context.Context) error {
	rows, err := s.r.Expired(ctx)
	if err != nil {
		return err
	}
	for _, o := range rows {
		_, err = s.rpc.Call(ctx, "checkout", platform.Command{Action: "cancel", ID: o.ID, UserID: o.UserID, Role: "system"})
		if err != nil && status.Code(err) != codes.FailedPrecondition {
			return err
		}
	}
	return nil
}
