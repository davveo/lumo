package usecase

import (
	"context"
	"errors"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		request := platform.Request{ID: row.ID, UserID: row.UserID, Role: constants.RoleSystem, Data: map[string]any{}}
		switch row.Status {
		case constants.AftersaleStatusInspecting:
			request.Data["condition"] = row.Condition
			_, err = s.Inspect(ctx, request)
		case constants.AftersaleStatusRefundRequested:
			_, err = s.Refund(ctx, request)
		case constants.AftersaleStatusExchangePreparing:
			_, err = s.Exchange(ctx, request)
		default:
			err = status.Error(codes.Unimplemented, "unsupported aftersale action")
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
