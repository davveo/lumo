package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/flash/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceFlash, map[string]platform.Operation{
		constants.ActionList:    s.List,
		constants.ActionGet:     s.Get,
		constants.ActionRequest: s.Request,
	}, nil)
}
