package usecase

import (
	"context"
	"errors"
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
func (s *Service) Topics() []string                              { return []string{"PaymentRejected", "GroupOrderTerminated"} }
func (s *Service) Tick(context.Context) error                    { return nil }
func own(c platform.Command, p domain.Payment) error {
	if p.UserID != c.UserID && c.Role != "admin" && c.Role != "system" {
		return status.Error(codes.PermissionDenied, "payment belongs to another member")
	}
	return nil
}
func (s *Service) Handle(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "payment.usecase."+c.Action)
	defer sp.End()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	switch c.Action {
	case "get":
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
	case "list":
		if c.Role != "admin" && c.Role != "system" {
			return nil, status.Error(codes.PermissionDenied, "internal payment listing")
		}
		rows, err := s.r.List(ctx, c.UserID, true)
		return platform.Result{"items": rows}, err
	case "create":
		return s.create(ctx, c)
	case "confirm_demo":
		if err := platform.RequireAdmin(c); err != nil {
			return nil, err
		}
		if os.Getenv("DEMO_MODE") != "true" && os.Getenv("DEMO_MODE") != "1" {
			return nil, status.Error(codes.FailedPrecondition, "demo payment adapter disabled")
		}
		// Every mutation takes the order gate before the payment row. The same
		// lock order is used by create/close, avoiding opposite-order deadlocks.
		before, err := s.r.Get(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		var out platform.Result
		err = s.r.Transaction(ctx, func(tx *repository.Tx) error {
			g, err := tx.Gate(before.OrderID)
			if err != nil {
				return err
			}
			p, err := tx.Get(c.ID)
			if err != nil {
				return err
			}
			if p.Status == "SUCCEEDED" {
				out = platform.Map(p)
				return nil
			}
			if g.Frozen || p.Status != "PENDING" {
				return status.Error(codes.FailedPrecondition, "payment is closed")
			}
			now := time.Now().UTC()
			p.Status = "SUCCEEDED"
			p.PaidAt = &now
			p.Receipt = "demo-receipt-" + p.ID
			if err = tx.Save(&p); err != nil {
				return err
			}
			if err = tx.Emit(platform.Event{ID: "PaymentSucceeded-" + p.ID, Type: "PaymentSucceeded", AggregateID: p.ID, UserID: p.UserID, Data: map[string]any{"payment_id": p.ID, "order_id": p.OrderID, "amount_cents": p.Amount, "paid_at": now.Format(time.RFC3339Nano), "provider": "demo"}}); err != nil {
				return err
			}
			out = platform.Map(p)
			return nil
		})
		return out, err
	case "close":
		if c.Role != "system" && c.Role != "admin" {
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
			if p.Status == "SUCCEEDED" {
				out = platform.Map(p)
				out["paid"] = true
				out["payment_id"] = p.ID
				return nil
			}
			p.Status = "CLOSED"
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
func (s *Service) create(ctx context.Context, c platform.Command) (platform.Result, error) {
	if os.Getenv("DEMO_MODE") != "true" && os.Getenv("DEMO_MODE") != "1" {
		return nil, status.Error(codes.FailedPrecondition, "no production payment adapter configured")
	}
	o, err := s.rpc.Call(ctx, "order", platform.Command{Action: "get", ID: c.ID, UserID: c.UserID, Role: c.Role})
	if err != nil {
		return nil, err
	}
	if platform.Text(o, "status") != "PENDING_PAYMENT" {
		return nil, status.Error(codes.FailedPrecondition, "order cannot be paid")
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
		p = domain.Payment{ID: platform.ID("pay"), OrderID: c.ID, UserID: platform.Text(o, "user_id"), Amount: platform.Int(o, "payable_cents"), Status: "PENDING", Provider: "demo"}
		if err = tx.Create(&p); err != nil {
			return err
		}
		out = platform.Map(p)
		return nil
	})
	return out, err
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type != "PaymentRejected" && e.Type != "GroupOrderTerminated" {
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
	if p.Status != "SUCCEEDED" {
		return status.Error(codes.FailedPrecondition, "cannot refund unconfirmed payment")
	}
	_, err = s.rpc.Call(ctx, "refund", platform.Command{Action: "create", ID: "automatic-" + p.ID, UserID: p.UserID, Role: "system", Data: map[string]any{"payment_id": p.ID, "order_id": p.OrderID, "amount_cents": p.Amount, "reason": e.Type}})
	return err
}
