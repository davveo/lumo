package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/group/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceGroup, map[string]platform.Operation{
		constants.ActionList:    s.List,
		constants.ActionGet:     s.Get,
		constants.ActionCreate:  s.Create,
		constants.ActionReserve: s.Reserve,
		constants.ActionPaid:    s.Paid,
		constants.ActionCancel:  s.Cancel,
		constants.ActionExpire:  s.Expire,
	}, nil)
}
