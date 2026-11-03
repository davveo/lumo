package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/fulfillment/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceFulfillment, map[string]platform.Operation{
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
