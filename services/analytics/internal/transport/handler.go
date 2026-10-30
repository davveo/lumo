package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceAnalytics, map[string]platform.Operation{
		constants.ActionAdminList: s.AdminList,
		constants.ActionRecord:    s.Record,
		constants.ActionList:      s.List,
	}, nil)
}
