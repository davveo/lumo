package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/logistics/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceLogistics, map[string]platform.Operation{
		constants.ActionQuote:   s.Quote,
		constants.ActionList:    s.List,
		constants.ActionCreate:  s.Create,
		constants.ActionGet:     s.Get,
		constants.ActionDeliver: s.Deliver,
	}, nil)
}
