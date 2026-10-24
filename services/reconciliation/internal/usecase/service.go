package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/reconciliation/internal/domain"
	"github.com/davveo/lumo/services/reconciliation/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	r   *repository.Repository
	rpc platform.Caller
}

func New(r *repository.Repository, rpc platform.Caller) *Service { return &Service{r, rpc} }
func (s *Service) Topics() []string                              { return nil }
func (s *Service) OnEvent(context.Context, platform.Event) error { return nil }
func (s *Service) Tick(context.Context) error                    { return nil }
func (s *Service) Handle(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "reconciliation.usecase."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireAdmin(c); err != nil {
		return nil, err
	}
	switch c.Action {
	case constants.ActionList:
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
	case constants.ActionRun:
		payments, err := s.rpc.Call(
			ctx,
			constants.ServicePayment,
			platform.Command{Action: constants.ActionList, UserID: c.UserID, Role: constants.RoleSystem},
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
			o, e := s.rpc.Call(
				ctx,
				constants.ServiceOrder,
				platform.Command{
					Action: constants.ActionGet,
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
		run := domain.Run{ID: platform.ID("recon"), Status: state, Details: string(raw)}
		if err = s.r.Create(ctx, &run); err != nil {
			return nil, err
		}
		return platform.Result{"id": run.ID, "status": state, "details": details}, nil
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported reconciliation action")
	}
}
