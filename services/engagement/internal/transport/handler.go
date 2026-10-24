package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceEngagement, map[string]platform.Operation{
		constants.ActionList:   s.List,
		constants.ActionPut:    s.Put,
		constants.ActionDelete: s.Delete,
		constants.ActionClear:  s.Clear,
	}, nil)
}
