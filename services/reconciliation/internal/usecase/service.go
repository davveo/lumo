package usecase

import (
	"context"
	"encoding/json"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/reconciliation/internal/domain"
	"github.com/davveo/lumo/services/reconciliation/internal/repository"
)

type Service struct {
	r   *repository.Repository
	rpc platform.Caller
}

func New(r *repository.Repository, rpc platform.Caller) *Service { return &Service{r, rpc} }
func (s *Service) Topics() []string                              { return nil }
func (s *Service) OnEvent(context.Context, platform.Event) error { return nil }
func (s *Service) Tick(ctx context.Context) error                { return s.process(ctx) }
func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "reconciliation.usecase.list")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireAdmin(c); err != nil {
		return nil, err
	}

	rows, err := s.r.List(ctx)
	if err != nil {
		return nil, err
	}
	items := []platform.Result{}
	for _, r := range rows {
		v := platform.Map(r)
		var d any
		_ = json.Unmarshal([]byte(r.Details), &d)
		v["details"] = d
		items = append(items, v)
	}
	return platform.Result{"items": items}, nil
}

func (s *Service) Run(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "reconciliation.usecase.run")
	defer sp.End()
	if retry, _ := c.Data["retry"].(bool); retry {
		if e := platform.RequireAdmin(c); e != nil {
			return nil, e
		}
		if strings.TrimSpace(platform.Text(c.Data, "reason")) == "" || len([]rune(platform.Text(c.Data, "reason"))) > 240 || c.ID == "" || c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 {
			return nil, status.Error(codes.InvalidArgument, "resume reason and idempotency key required")
		}
		return s.r.Resume(ctx, c)
	}
	if async, _ := c.Data["async"].(bool); async || platform.Text(c.Data, "from") != "" {
		return s.start(ctx, c)
	}
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireAdmin(c); err != nil {
		return nil, err
	}

	payments, err := platform.NewClients(s.rpc).Payment.List(
		ctx, platform.Request{UserID: c.UserID, Role: constants.RoleSystem},
	)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(payments["items"])
	var rows []map[string]any
	if err = json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	diffs := []platform.Result{}
	for _, p := range rows {
		if platform.Text(p, "status") != constants.PaymentStatusSucceeded {
			continue
		}
		o, e := platform.NewClients(s.rpc).Order.Get(
			ctx, platform.Request{
				ID:     platform.Text(p, "order_id"),
				UserID: c.UserID,
				Role:   constants.RoleSystem,
			},
		)
		if e != nil {
			diffs = append(
				diffs,
				platform.Result{
					"payment_id": p["id"],
					"kind":       constants.ReconciliationDifferenceKindOrderUnavailable,
				},
			)
			continue
		}
		if platform.Int(o, "payable_cents") != platform.Int(p, "amount_cents") {
			diffs = append(
				diffs,
				platform.Result{
					"payment_id": p["id"],
					"kind":       constants.ReconciliationDifferenceKindAmountMismatch,
				},
			)
		}
		if platform.Text(o, "payment_id") != platform.Text(p, "id") {
			diffs = append(
				diffs,
				platform.Result{
					"payment_id": p["id"],
					"kind":       constants.ReconciliationDifferenceKindPaymentNotAcceptedOrPendingEvent,
				},
			)
		}
	}
	details := platform.Result{
		"checked_payments": len(rows),
		"differences":      diffs,
		"scope":            "internal-demo-ledgers; external channel bill adapter not configured",
	}
	raw, _ := json.Marshal(details)
	state := constants.ReconciliationStatusMatched
	if len(diffs) > 0 {
		state = constants.ReconciliationStatusDifferences
	}
	id, err := s.r.BusinessID(ctx, "recon", "", "")
	if err != nil {
		return nil, err
	}
	run := domain.Run{ID: id, Status: state, Details: string(raw)}
	if err = s.r.Create(ctx, &run); err != nil {
		return nil, err
	}
	return platform.Result{"id": run.ID, "status": state, "details": details}, nil
}
