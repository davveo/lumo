package usecase

import (
	"context"
	"errors"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) Tick(ctx context.Context) (traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.fulfillment.RecoverPending")
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
		request := platform.Request{ID: row.ID, UserID: row.UserID, Role: constants.RoleSystem}
		if row.Status == constants.FulfillmentStatusDispatching {
			_, err = s.Dispatch(ctx, request)
		} else {
			_, err = s.Cancel(ctx, request)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
