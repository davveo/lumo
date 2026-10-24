package points_test

import (
	"context"
	"testing"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIndependentConfirmAndCancelPreserveLedger(t *testing.T) {
	_, m := setup(t, nil)
	ctx := context.Background()
	req := platform.Request{UserID: "demo", Role: constants.RoleSystem}
	call := func(action string, want string) platform.Result {
		t.Helper()
		v, err := m.Operations[action](ctx, req)
		if err != nil || (want != "" && platform.Text(v, "state") != want) {
			t.Fatalf("direct %s = %#v, %v; want state %s", action, v, err, want)
		}
		return v
	}
	req.ID = "confirm-direct"
	req.Data = map[string]any{"amount": int64(300), "action": constants.ActionCancel}
	call(constants.ActionReserve, constants.PointsHoldStateReserved)
	call(constants.ActionConfirm, constants.PointsHoldStateConfirmed)
	call(constants.ActionConfirm, constants.PointsHoldStateConfirmed)
	if _, err := m.Operations[constants.ActionCancel](ctx, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("cancel changed confirmed hold: %v", err)
	}
	req.ID = "cancel-direct"
	req.Data = map[string]any{"amount": int64(200), "action": constants.ActionConfirm}
	call(constants.ActionReserve, constants.PointsHoldStateReserved)
	call(constants.ActionCancel, constants.PointsHoldStateCancelled)
	call(constants.ActionCancel, constants.PointsHoldStateCancelled)
	if _, err := m.Operations[constants.ActionConfirm](ctx, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("confirm changed cancelled hold: %v", err)
	}
	req.ID = "tombstone-direct"
	call(constants.ActionCancel, constants.PointsHoldStateCancelled)
	if _, err := m.Operations[constants.ActionReserve](ctx, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reserve ignored cancel tombstone: %v", err)
	}
	req.Role = constants.RoleUser
	if _, err := m.Operations[constants.ActionConfirm](ctx, req); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("direct confirm skipped internal role check: %v", err)
	}
	result := call(constants.ActionGet, "")
	if platform.Int(result, "available") != 4700 || platform.Int(result, "frozen") != 0 {
		t.Fatalf("ledger moved balances more than once: %#v", result)
	}
}
