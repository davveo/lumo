package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceIam, map[string]platform.Operation{
		constants.ActionLogin:     s.Login,
		constants.ActionValidate:  s.Validate,
		constants.ActionRefresh:   s.Refresh,
		constants.ActionLogout:    s.Logout,
		constants.ActionLogoutAll: s.LogoutAll,
	}, nil)
}
