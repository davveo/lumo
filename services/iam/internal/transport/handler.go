package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceIAM, map[string]platform.Operation{constants.ActionOAuthStart: s.OAuthStart, constants.ActionOAuthCallback: s.OAuthCallback, constants.ActionLoginMethods: s.LoginMethods, constants.ActionPolicyGet: s.PolicyGet, constants.ActionPolicyUpsert: s.PolicyUpsert, constants.ActionOtpSend: s.OtpSend, constants.ActionOtpLogin: s.OtpLogin,
		constants.ActionAuditList: s.AuditList, constants.ActionAdminGet: s.AdminGet,
		constants.ActionSetActive:    s.SetActive,
		constants.ActionAdminList:    s.AdminList,
		constants.ActionAdminSummary: s.AdminSummary,
		constants.ActionLogin:        s.Login,
		constants.ActionValidate:     s.Validate,
		constants.ActionRefresh:      s.Refresh,
		constants.ActionLogout:       s.Logout,
		constants.ActionLogoutAll:    s.LogoutAll,
	}, nil)
}
