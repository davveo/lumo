package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/flash/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceFlash, map[string]platform.Operation{
		constants.ActionAdminList:     s.AdminList,
		constants.ActionUpsert:        s.Upsert,
		constants.ActionSessionGet:    s.SessionGet,
		constants.ActionRequestsAdmin: s.RequestsAdmin,

		constants.ActionList:    s.List,
		constants.ActionGet:     s.Get,
		constants.ActionRequest: s.Request,
	}, nil)
}
