package usecase

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/invoice/internal/domain"
	"github.com/davveo/lumo/services/invoice/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
	"regexp"
	"strings"
)

type Service struct {
	Repo *repository.Repository
	RPC  platform.Caller
}

func (s *Service) Topics() []string { return []string{constants.EventRefundSucceeded} }
func (s *Service) OnEvent(ctx context.Context, e platform.Event) (traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.invoice.event."+e.Type)
	defer span.End()
	platform.TraceRequest(ctx, e)
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
	if e.Type != constants.EventRefundSucceeded {
		return nil
	}
	id := platform.Text(e.Data, "refund_id")
	if id == "" {
		id = e.AggregateID
	}
	orderID := platform.Text(e.Data, "order_id")
	if id == "" || orderID == "" {
		return status.Error(codes.InvalidArgument, "refund event requires identities")
	}
	return s.Repo.ApplyRefund(
		ctx,
		orderID,
		id,
		e.UserID,
		platform.Int(e.Data, "amount_cents"),
		platform.Int(e.Data, "refunded_total_cents"),
	)
}
func privileged(c platform.Request) bool {
	return c.Role == constants.RoleAdmin || c.Role == constants.RoleSystem
}
func (s *Service) AdjustDemo(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.invoice."+constants.ActionAdjustDemo)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	row, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if !privileged(c) && row.UserID != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "invoice ownership mismatch")
	}

	if c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "invoice staff permission required")
	}
	if os.Getenv("DEMO_MODE") != "true" {
		return nil, status.Error(codes.FailedPrecondition, "demo invoice provider disabled")
	}
	row, err = s.Repo.CompleteDemoAdjustments(ctx, row.ID)
	return platform.Map(row), err

}

func (s *Service) Create(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.invoice."+constants.ActionCreate)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	return s.create(ctx, c)

}

func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.invoice."+constants.ActionGet)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	row, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if !privileged(c) && row.UserID != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "invoice ownership mismatch")
	}

	return platform.Map(row), nil

}

func (s *Service) IssueDemo(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.invoice."+constants.ActionIssueDemo)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	row, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if !privileged(c) && row.UserID != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "invoice ownership mismatch")
	}

	if c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "invoice staff permission required")
	}
	if os.Getenv("DEMO_MODE") != "true" {
		return nil, status.Error(codes.FailedPrecondition, "demo invoice provider disabled")
	}
	if row.Status == constants.InvoiceStatusIssued {
		return platform.Map(row), nil
	}
	order, err := s.RPC.Call(
		ctx,
		constants.ServiceOrder,
		platform.Command{
			Action: constants.ActionGet,
			ID:     row.OrderID,
			UserID: row.UserID,
			Role:   constants.RoleSystem,
		},
	)
	if err != nil {
		return nil, err
	}
	state := platform.Text(order, "status")
	if state != constants.OrderStatusReadyToFulfill && state != constants.OrderStatusShipped &&
		state != constants.OrderStatusCompleted {
		return nil, status.Error(codes.FailedPrecondition, "order is not ready for invoicing")
	}
	if total := refundTotal(order); total > row.RefundedCents {
		if err = s.Repo.ApplyRefund(
			ctx,
			row.OrderID,
			fmt.Sprintf("snapshot:%s:%d", row.OrderID, total),
			row.UserID,
			0,
			total,
		); err != nil {
			return nil, err
		}
	}
	sum := sha256.Sum256([]byte(row.ID))
	number := fmt.Sprintf("DEMO-INVOICE-%X", sum[:10])
	row, err = s.Repo.IssueDemo(ctx, row.ID, number)
	return platform.Map(row), err

}

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.invoice."+constants.ActionList)
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
	if privileged(c) {
		user = ""
	}
	rows, err := s.Repo.List(ctx, user)
	return platform.Result{"items": rows}, err

}

func (s *Service) Reversals(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.invoice."+constants.ActionReversals)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	row, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if !privileged(c) && row.UserID != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "invoice ownership mismatch")
	}

	rows, err := s.Repo.Reversals(ctx, row.ID)
	return platform.Result{"items": rows}, err

}

func (s *Service) create(ctx context.Context, c platform.Request) (platform.Result, error) {
	orderID := platform.Text(c.Data, "order_id")
	title := strings.TrimSpace(platform.Text(c.Data, "title"))
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.InvoiceKindPersonal
	}
	taxID := strings.ToUpper(strings.TrimSpace(platform.Text(c.Data, "tax_id")))
	if orderID == "" || title == "" || len(title) > 200 ||
		(kind != constants.InvoiceKindPersonal && kind != constants.InvoiceKindCompany) {
		return nil, status.Error(codes.InvalidArgument, "order, valid title and invoice kind required")
	}
	if kind == constants.InvoiceKindCompany && !regexp.MustCompile(`^[A-Z0-9]{18}$`).MatchString(taxID) {
		return nil, status.Error(codes.InvalidArgument, "company tax_id must contain 18 letters or digits")
	}
	order, err := s.RPC.Call(
		ctx,
		constants.ServiceOrder,
		platform.Command{Action: constants.ActionGet, ID: orderID, UserID: c.UserID, Role: c.Role},
	)
	if err != nil {
		return nil, err
	}
	user := platform.Text(order, "user_id")
	if user == "" || (!privileged(c) && c.UserID != user) {
		return nil, status.Error(codes.PermissionDenied, "order ownership mismatch")
	}
	state := platform.Text(order, "status")
	if state != constants.OrderStatusReadyToFulfill && state != constants.OrderStatusShipped &&
		state != constants.OrderStatusCompleted &&
		state != constants.OrderStatusPaidWaitGroup &&
		state != constants.OrderStatusPaidConfirming {
		return nil, status.Error(codes.FailedPrecondition, "order is not paid")
	}
	refunded := refundTotal(order)
	amount := platform.Int(order, "payable_cents")
	if amount-refunded <= 0 {
		return nil, status.Error(codes.FailedPrecondition, "no invoiceable amount")
	}
	row := &domain.Record{
		ID:            "inv:" + orderID,
		OrderID:       orderID,
		UserID:        user,
		Kind:          kind,
		Title:         title,
		TaxID:         taxID,
		Status:        constants.InvoiceStatusRequested,
		AmountCents:   amount,
		RefundedCents: refunded,
		Provider:      constants.InvoiceProviderUnsubmitted,
	}
	if err = s.Repo.Create(ctx, row); err != nil {
		return nil, err
	}
	// A refund may have arrived before this invoice existed. Re-read the owner
	// snapshot; cumulative totals also make a later duplicated event harmless.
	fresh, err := s.RPC.Call(
		ctx,
		constants.ServiceOrder,
		platform.Command{Action: constants.ActionGet, ID: orderID, UserID: user, Role: constants.RoleSystem},
	)
	if err != nil {
		return nil, err
	}
	if total := refundTotal(fresh); total > row.RefundedCents {
		if err = s.Repo.ApplyRefund(
			ctx,
			orderID,
			fmt.Sprintf("snapshot:%s:%d", orderID, total),
			user,
			0,
			total,
		); err != nil {
			return nil, err
		}
		row, err = s.Repo.Get(ctx, row.ID)
		if err != nil {
			return nil, err
		}
	}
	return platform.Map(row), nil
}

func refundTotal(order platform.Result) int64 {
	n := platform.Int(order, "refunded_cents")
	if x := platform.Int(order, "refunded_total_cents"); x > n {
		n = x
	}
	return n
}
