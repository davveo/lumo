package usecase

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/payment/internal/domain"
	"github.com/davveo/lumo/services/payment/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"os"
	"time"
)

type Service struct {
	r   *repository.Repository
	rpc platform.Caller
}

func New(r *repository.Repository, rpc platform.Caller) *Service { return &Service{r, rpc} }
func (s *Service) Topics() []string {
	return []string{constants.EventPaymentRejected, constants.EventGroupOrderTerminated}
}
func (s *Service) Tick(context.Context) error { return nil }
func own(c platform.Command, p domain.Payment) error {
	if p.UserID != c.UserID && c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
		return status.Error(codes.PermissionDenied, "payment belongs to another member")
	}
	return nil
}
func (s *Service) Handle(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "payment.usecase."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	switch c.Action {
	case constants.ActionGet:
		p, err := s.r.Get(ctx, c.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "payment not found")
		}
		if err != nil {
			return nil, err
		}
		if err = own(c, p); err != nil {
			return nil, err
		}
		return platform.Map(p), nil
	case constants.ActionList:
		if c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
			return nil, status.Error(codes.PermissionDenied, "internal payment listing")
		}
		rows, err := s.r.List(ctx, c.UserID, true)
		return platform.Result{"items": rows}, err
	case constants.ActionCreate:
		return s.create(ctx, c)
	case constants.ActionSubmitDemo:
		return s.confirmDemo(ctx, c)
	case constants.ActionConfirmDemo:
		if err := platform.RequireAdmin(c); err != nil {
			return nil, err
		}
		return s.confirmDemo(ctx, c)
	case constants.ActionClose:
		if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
			return nil, status.Error(codes.PermissionDenied, "internal payment close")
		}
		var out platform.Result
		err := s.r.Transaction(ctx, func(tx *repository.Tx) error {
			g, err := tx.Gate(c.ID)
			if err != nil {
				return err
			}
			if err = tx.Freeze(&g); err != nil {
				return err
			}
			p, err := tx.ByOrder(c.ID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				out = platform.Result{"closed": true, "paid": false, "order_id": c.ID}
				return nil
			}
			if err != nil {
				return err
			}
			if p.Status == constants.PaymentStatusSucceeded {
				out = platform.Map(p)
				out["paid"] = true
				out["payment_id"] = p.ID
				return nil
			}
			p.Status = constants.PaymentStatusClosed
			if err = tx.Save(&p); err != nil {
				return err
			}
			out = platform.Result{"closed": true, "paid": false, "order_id": c.ID}
			return nil
		})
		return out, err
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported payment action")
	}
}

// requirePayableOrder checks server-owned state and deadlines; browser data
// never supplies the captured amount, owner, provider, or success timestamp.
func requirePayableOrder(o platform.Result, now time.Time) error {
	if platform.Text(o, "status") != constants.OrderStatusPendingPayment {
		return status.Error(codes.FailedPrecondition, "order cannot be paid")
	}
	expires, err := time.Parse(time.RFC3339Nano, platform.Text(o, "expires_at"))
	if err != nil || !now.Before(expires) {
		return status.Error(codes.FailedPrecondition, "order payment deadline has expired")
	}
	return nil
}

func (s *Service) confirmDemo(ctx context.Context, c platform.Command) (platform.Result, error) {
	if os.Getenv("DEMO_MODE") != "true" && os.Getenv("DEMO_MODE") != "1" {
		return nil, status.Error(codes.FailedPrecondition, "demo payment adapter disabled")
	}
	before, err := s.r.Get(ctx, c.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, status.Error(codes.NotFound, "payment not found")
	}
	if err != nil {
		return nil, err
	}
	if err = own(c, before); err != nil {
		return nil, err
	}
	var out platform.Result
	// All payment mutations take the order gate before the payment row. The
	// same lock order is used by create/close to serialize payment and cancel.
	err = s.r.Transaction(ctx, func(tx *repository.Tx) error {
		g, err := tx.Gate(before.OrderID)
		if err != nil {
			return err
		}
		p, err := tx.Get(c.ID)
		if err != nil {
			return err
		}
		if err = own(c, p); err != nil {
			return err
		}
		if p.Provider != constants.PaymentProviderDemo {
			return status.Error(codes.FailedPrecondition, "payment is not a demo payment")
		}
		if p.Status == constants.PaymentStatusSucceeded {
			out = platform.Map(p)
			return nil
		}
		if g.Frozen || p.Status != constants.PaymentStatusPending {
			return status.Error(codes.FailedPrecondition, "payment is closed")
		}
		now := time.Now().UTC()
		if c.Action == constants.ActionSubmitDemo {
			// Hold the payment gate while observing order state so a concurrent
			// close cannot freeze the payment between validation and capture.
			o, err := s.rpc.Call(ctx, constants.ServiceOrder, platform.Command{Action: constants.ActionGet, ID: p.OrderID, UserID: c.UserID, Role: c.Role})
			if err != nil {
				return err
			}
			now = time.Now().UTC()
			if err = requirePayableOrder(o, now); err != nil {
				return err
			}
			if platform.Text(o, "user_id") != p.UserID || platform.Int(o, "payable_cents") != p.Amount {
				return status.Error(codes.FailedPrecondition, "payment does not match order")
			}
		}
		p.Status = constants.PaymentStatusSucceeded
		p.PaidAt = &now
		p.Receipt = "demo-receipt-" + p.ID
		if err = tx.Save(&p); err != nil {
			return err
		}
		if err = tx.Emit(platform.Event{ID: "PaymentSucceeded-" + p.ID, Type: constants.EventPaymentSucceeded, AggregateID: p.ID, UserID: p.UserID, Data: map[string]any{"payment_id": p.ID, "order_id": p.OrderID, "amount_cents": p.Amount, "paid_at": now.Format(time.RFC3339Nano), "provider": constants.PaymentProviderDemo}}); err != nil {
			return err
		}
		out = platform.Map(p)
		return nil
	})
	return out, err
}

func (s *Service) create(ctx context.Context, c platform.Command) (platform.Result, error) {
	if os.Getenv("DEMO_MODE") != "true" && os.Getenv("DEMO_MODE") != "1" {
		return nil, status.Error(codes.FailedPrecondition, "no production payment adapter configured")
	}
	o, err := s.rpc.Call(ctx, constants.ServiceOrder, platform.Command{Action: constants.ActionGet, ID: c.ID, UserID: c.UserID, Role: c.Role})
	if err != nil {
		return nil, err
	}
	if err := requirePayableOrder(o, time.Now().UTC()); err != nil {
		return nil, err
	}
	var out platform.Result
	err = s.r.Transaction(ctx, func(tx *repository.Tx) error {
		g, err := tx.Gate(c.ID)
		if err != nil {
			return err
		}
		if g.Frozen {
			return status.Error(codes.FailedPrecondition, "payment creation frozen by close")
		}
		p, err := tx.ByOrder(c.ID)
		if err == nil {
			if err = own(c, p); err != nil {
				return err
			}
			out = platform.Map(p)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		p = domain.Payment{ID: platform.ID("pay"), OrderID: c.ID, UserID: platform.Text(o, "user_id"), Amount: platform.Int(o, "payable_cents"), Status: constants.PaymentStatusPending, Provider: constants.PaymentProviderDemo}
		if err = tx.Create(&p); err != nil {
			return err
		}
		out = platform.Map(p)
		return nil
	})
	return out, err
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type != constants.EventPaymentRejected && e.Type != constants.EventGroupOrderTerminated {
		return nil
	}
	pid := platform.Text(e.Data, "payment_id")
	if pid == "" {
		return nil
	}
	p, err := s.r.Get(ctx, pid)
	if err != nil {
		return err
	}
	if p.Status != constants.PaymentStatusSucceeded {
		return status.Error(codes.FailedPrecondition, "cannot refund unconfirmed payment")
	}
	_, err = s.rpc.Call(ctx, constants.ServiceRefund, platform.Command{Action: constants.ActionCreate, ID: "automatic-" + p.ID, UserID: p.UserID, Role: constants.RoleSystem, Data: map[string]any{"payment_id": p.ID, "order_id": p.OrderID, "amount_cents": p.Amount, "reason": e.Type}})
	return err
}
