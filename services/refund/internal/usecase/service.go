package usecase

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/refund/internal/domain"
	"github.com/davveo/lumo/services/refund/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"os"
)

type Service struct {
	r   *repository.Repository
	rpc platform.Caller
}

func New(r *repository.Repository, rpc platform.Caller) *Service { return &Service{r, rpc} }
func (s *Service) Topics() []string                              { return nil }
func (s *Service) Tick(context.Context) error                    { return nil }
func (s *Service) OnEvent(context.Context, platform.Event) error { return nil }
func own(c platform.Command, r domain.Refund) error {
	if r.UserID != c.UserID && c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return status.Error(codes.PermissionDenied, "refund belongs to another member")
	}
	return nil
}
func (s *Service) Handle(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "refund.usecase."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	switch c.Action {
	case constants.ActionGet:
		r, err := s.r.Get(ctx, c.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "refund not found")
		}
		if err != nil {
			return nil, err
		}
		if err = own(c, r); err != nil {
			return nil, err
		}
		return platform.Map(r), nil
	case constants.ActionList:
		if c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
			return nil, status.Error(codes.PermissionDenied, "internal refund listing")
		}
		rows, err := s.r.List(ctx, c.UserID, true)
		return platform.Result{"items": rows}, err
	case constants.ActionCreate:
		return s.create(ctx, c)
	case constants.ActionConfirmDemo:
		if err := platform.RequireAdmin(c); err != nil {
			return nil, err
		}
		if os.Getenv("DEMO_MODE") != "true" && os.Getenv("DEMO_MODE") != "1" {
			return nil, status.Error(codes.FailedPrecondition, "demo refund adapter disabled")
		}
		r, err := s.r.Get(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		var out platform.Result
		err = s.r.Transaction(ctx, func(tx *repository.Tx) error {
			cap, err := tx.Cap(r.PaymentID, 0)
			if err != nil {
				return err
			}
			current, err := tx.Get(c.ID)
			if err != nil {
				return err
			}
			if current.Status == constants.RefundStatusSucceeded {
				out = platform.Map(current)
				return nil
			}
			if current.Status != constants.RefundStatusProcessing {
				return status.Error(codes.FailedPrecondition, "refund not processing")
			}
			if cap.Reserved < current.Amount {
				return status.Error(codes.Internal, "refund cap ledger inconsistent")
			}
			cap.Reserved -= current.Amount
			cap.Completed += current.Amount
			current.Status = constants.RefundStatusSucceeded
			if err = tx.SaveCap(&cap); err != nil {
				return err
			}
			if err = tx.Save(&current); err != nil {
				return err
			}
			data := map[string]any{"refund_id": current.ID, "order_id": current.OrderID, "payment_id": current.PaymentID, "amount_cents": current.Amount, "payable_cents": cap.Paid, "refunded_total_cents": cap.Completed, "aftersale_id": current.AftersaleID, "reason": current.Reason}
			if err = tx.Emit(platform.Event{ID: "RefundSucceeded-" + current.ID, Type: constants.EventRefundSucceeded, AggregateID: current.ID, UserID: current.UserID, Data: data}); err != nil {
				return err
			}
			out = platform.Map(current)
			out["refunded_total_cents"] = cap.Completed
			return nil
		})
		return out, err
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported refund action")
	}
}
func (s *Service) create(ctx context.Context, c platform.Command) (platform.Result, error) {
	if os.Getenv("DEMO_MODE") != "true" && os.Getenv("DEMO_MODE") != "1" {
		return nil, status.Error(codes.FailedPrecondition, "no production refund adapter configured")
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "refund requires approved business command")
	}
	amount := platform.Int(c.Data, "amount_cents")
	pid := platform.Text(c.Data, "payment_id")
	oid := platform.Text(c.Data, "order_id")
	if c.ID == "" || amount <= 0 || pid == "" || oid == "" {
		return nil, status.Error(codes.InvalidArgument, "refund ID, payment, order and positive amount required")
	}
	p, err := s.rpc.Call(ctx, constants.ServicePayment, platform.Command{Action: constants.ActionGet, ID: pid, UserID: c.UserID, Role: constants.RoleSystem})
	if err != nil {
		return nil, err
	}
	if platform.Text(p, "status") != constants.RefundStatusSucceeded || platform.Text(p, "order_id") != oid {
		return nil, status.Error(codes.FailedPrecondition, "original payment not captured or wrong order")
	}
	if c.Role != constants.RoleAdmin && platform.Text(p, "user_id") != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "refund payment owner mismatch")
	}
	var out platform.Result
	err = s.r.Transaction(ctx, func(tx *repository.Tx) error {
		cap, err := tx.Cap(pid, platform.Int(p, "amount_cents"))
		if err != nil {
			return err
		}
		existing, e := tx.Get(c.ID)
		if e == nil {
			if existing.OrderID != oid || existing.PaymentID != pid || existing.Amount != amount {
				return status.Error(codes.AlreadyExists, "refund idempotency key reused with different request")
			}
			out = platform.Map(existing)
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if cap.Reserved+cap.Completed+amount > cap.Paid {
			return status.Error(codes.FailedPrecondition, "refund amount exceeds unrefunded capture")
		}
		cap.Reserved += amount
		r := domain.Refund{ID: c.ID, OrderID: oid, PaymentID: pid, UserID: platform.Text(p, "user_id"), AftersaleID: platform.Text(c.Data, "aftersale_id"), Amount: amount, Status: constants.RefundStatusProcessing, Reason: platform.Text(c.Data, "reason"), Provider: constants.RefundProviderDemo}
		if err = tx.SaveCap(&cap); err != nil {
			return err
		}
		if err = tx.Create(&r); err != nil {
			return err
		}
		out = platform.Map(r)
		return nil
	})
	return out, err
}
