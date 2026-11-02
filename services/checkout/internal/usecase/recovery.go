package usecase

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func (s *Service) recoverOne(ctx context.Context, e platform.Event) error {
	row, err := s.r.Get(ctx, e.AggregateID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.UserID != e.UserID || row.State != platform.Text(e.Data, "expected_status") ||
		row.UpdatedAt.UnixMilli() != platform.Int(e.Data, "expected_version") {
		return nil
	}
	mode := constants.CheckoutModeCreate
	switch row.State {
	case constants.CheckoutStateCancelling:
		mode = constants.CheckoutModeRelease
	case constants.CheckoutStatePaidConfirming, constants.CheckoutStatePaidWaitGroup:
		mode = constants.CheckoutModePaid
	}
	if mode == constants.CheckoutModePaid {
		_, err = s.Paid(ctx, platform.Request{ID: row.ID, UserID: row.UserID, Role: constants.RoleSystem})
	} else {
		_, err = s.resumeWorkflow(ctx, row.ID, row.UserID, mode)
	}
	if status.Code(err) == codes.Aborted || status.Code(err) == codes.FailedPrecondition {
		return nil
	}
	return err
}
func (s *Service) resolveGroupOne(ctx context.Context, e platform.Event) error {
	row, err := s.r.Get(ctx, e.AggregateID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.UserID != e.UserID {
		return status.Error(codes.PermissionDenied, "group work owner mismatch")
	}
	req := platform.Request{ID: row.ID, UserID: row.UserID, Role: constants.RoleSystem}
	clients := platform.NewClients(s.rpc)
	if formed, _ := e.Data["formed"].(bool); formed {
		if _, err = clients.Order.GroupFormed(ctx, req); err != nil {
			return err
		}
		_, err = s.Paid(ctx, req)
		return err
	}
	if _, err = clients.Order.GroupFailed(ctx, req); err != nil {
		return err
	}
	if _, err = clients.Payment.Close(ctx, req); err != nil {
		return err
	}
	_, err = s.resumeWorkflow(ctx, row.ID, row.UserID, constants.CheckoutModeRelease)
	return err
}
