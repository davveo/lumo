package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/invoice/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceInvoice, map[string]platform.Operation{
		constants.ActionAdminList:    s.AdminList,
		constants.ActionAdminSummary: s.AdminSummary,
		constants.ActionList:         s.List,
		constants.ActionCreate:       s.Create,
		constants.ActionGet:          s.Get,
		constants.ActionReversals:    s.Reversals,
		constants.ActionIssueDemo:    s.IssueDemo,
		constants.ActionAdjustDemo:   s.AdjustDemo,
	}, nil)
}
