package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceInventory, map[string]platform.Operation{
		constants.ActionGet:      s.Get,
		constants.ActionBatchGet: s.BatchGet,
		constants.ActionReserve:  s.Reserve,
		constants.ActionConfirm:  s.Confirm,
		constants.ActionCancel:   s.Cancel,
		constants.ActionDispatch: s.Dispatch,
		constants.ActionRestock:  s.Restock,
		constants.ActionAdjust:   s.Adjust,
	}, nil)
}
