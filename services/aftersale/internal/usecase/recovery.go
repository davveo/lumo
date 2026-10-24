package usecase

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) Tick(ctx context.Context) (traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.RecoverPending")
	defer span.End()
	platform.TraceRequest(ctx, struct{}{})
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
	rows, err := s.Repo.Pending(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c := platform.Command{ID: row.ID, UserID: row.UserID, Role: constants.RoleSystem, Data: map[string]any{}}
		switch row.Status {
		case constants.AftersaleStatusInspecting:
			c.Action = constants.ActionInspect
			c.Data["condition"] = row.Condition
		case constants.AftersaleStatusRefundRequested:
			c.Action = constants.ActionRefund
		case constants.AftersaleStatusExchangePreparing:
			c.Action = constants.ActionExchange
		}
		if _, err = s.Handle(ctx, c); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
