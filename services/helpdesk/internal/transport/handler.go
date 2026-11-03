package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceHelpdesk, map[string]platform.Operation{
		constants.ActionAdminList:    s.AdminList,
		constants.ActionWorkQueue:    s.WorkQueue,
		constants.ActionWorkDetail:   s.WorkDetail,
		constants.ActionAssignWork:   s.AssignWork,
		constants.ActionSaveView:     s.SaveView,
		constants.ActionViews:        s.Views,
		constants.ActionPolicyGet:    s.PolicyGet,
		constants.ActionPolicyUpsert: s.PolicyUpsert,
		constants.ActionFaq:          s.Faq,
		constants.ActionList:         s.List,
		constants.ActionGet:          s.Get,
		constants.ActionCreate:       s.Create,
		constants.ActionReply:        s.Reply,
		constants.ActionClose:        s.Close,
	}, nil)
}
