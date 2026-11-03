package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/fulfillment/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceFulfillment, map[string]platform.Operation{
		constants.ActionDeliver:        s.Deliver,
		constants.ActionLineState:      s.LineState,
		constants.ActionHoldLines:      s.HoldLines,
		constants.ActionReleaseHold:    s.ReleaseHold,
		constants.ActionCancelLines:    s.CancelLines,
		constants.ActionParcelCreate:   s.ParcelCreate,
		constants.ActionParcelCancel:   s.ParcelCancel,
		constants.ActionCreateGift:     s.CreateGift,
		constants.ActionInspectReturns: s.InspectReturns,
		constants.ActionAdminList:      s.AdminList,
		constants.ActionAdminSummary:   s.AdminSummary,
		constants.ActionList:           s.List,
		constants.ActionGet:            s.Get,
		constants.ActionCreateExchange: s.CreateExchange,
		constants.ActionDispatch:       s.Dispatch,
		constants.ActionCancel:         s.Cancel,
		constants.ActionInspectReturn:  s.InspectReturn,
	}, nil)
}
