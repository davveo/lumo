package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceNotification, map[string]platform.Operation{
		constants.ActionTemplatesAdmin: s.TemplatesAdmin, constants.ActionTemplateUpsert: s.TemplateUpsert,
		constants.ActionList:    s.List,
		constants.ActionRead:    s.Read,
		constants.ActionReadAll: s.ReadAll,
	}, nil)
}
