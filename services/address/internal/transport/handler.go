package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/address/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceAddress, map[string]platform.Operation{
		constants.ActionList:    s.List,
		constants.ActionGet:     s.Get,
		constants.ActionCreate:  s.Create,
		constants.ActionUpdate:  s.Update,
		constants.ActionDelete:  s.Delete,
		constants.ActionDefault: s.Default,
	}, nil)
}
