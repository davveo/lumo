package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/interaction/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceInteraction, map[string]platform.Operation{
		constants.ActionList:     s.List,
		constants.ActionCreate:   s.Create,
		constants.ActionModerate: s.Moderate,
		constants.ActionAnswer:   s.Answer,
		constants.ActionLike:     s.Like,
	}, nil)
}
