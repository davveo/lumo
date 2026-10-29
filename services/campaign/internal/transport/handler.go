package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/campaign/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceCampaign, map[string]platform.Operation{
		constants.ActionRuleUpsert: s.RuleUpsert,

		constants.ActionList:      s.List,
		constants.ActionAdminList: s.AdminList,
		constants.ActionUpsert:    s.Upsert,
		constants.ActionEvaluate:  s.Evaluate,
		constants.ActionReserve:   s.Reserve,
		constants.ActionConfirm:   s.Confirm,
		constants.ActionCancel:    s.Cancel,
	}, nil)
}
