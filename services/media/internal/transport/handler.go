package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/media/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceMedia, map[string]platform.Operation{
		constants.ActionCreate:   s.Create,
		constants.ActionGet:      s.Get,
		constants.ActionDownload: s.Download,
		constants.ActionComplete: s.Complete,
	}, nil)
}
