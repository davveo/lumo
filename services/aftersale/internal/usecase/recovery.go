package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Tick(ctx context.Context) error {
	rows, err := s.Repo.Pending(ctx)
	if err != nil {
		return err
	}
	events := []platform.Event{}
	for _, row := range rows {
		events = append(
			events,
			platform.RecoveryEvent(
				constants.EventAftersaleRecoveryRequested,
				row.ID,
				row.UserID,
				row.Status,
				row.Version,
			),
		)
	}
	return s.Repo.QueueWork(ctx, events)
}
func (s *Service) recoverOne(ctx context.Context, e platform.Event) error {
	row, err := s.Repo.Get(ctx, e.AggregateID)
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if row.UserID != e.UserID || row.Status != platform.Text(e.Data, "expected_status") ||
		row.Version != platform.Int(e.Data, "expected_version") {
		return nil
	}
	req := platform.Request{ID: row.ID, UserID: row.UserID, Role: constants.RoleSystem, Data: map[string]any{}}
	switch row.Status {
 case constants.AftersaleStatusApproving:_,err=s.approveRefund(ctx,row)
 case constants.AftersaleStatusCancelling:_,err=s.releaseRefund(ctx,row,false)
 case constants.AftersaleStatusRejecting:_,err=s.releaseRefund(ctx,row,true)
	case constants.AftersaleStatusInspecting:
		req.Data["condition"] = row.Condition
		_, err = s.Inspect(ctx, req)
	case constants.AftersaleStatusRefundRequested:
		_, err = s.Refund(ctx, req)
	case constants.AftersaleStatusExchangePreparing:
		_, err = s.Exchange(ctx, req)
	}
	return err
}
