package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceCoupon, map[string]platform.Operation{
		constants.ActionTemplates:         s.Templates,
		constants.ActionTemplatesSnapshot: s.TemplatesSnapshot,
		constants.ActionTemplatesByIds:    s.TemplatesByIds,
		constants.ActionList:              s.List,
		constants.ActionClaim:             s.Claim,
		constants.ActionReserve:           s.Reserve,
		constants.ActionConfirm:           s.Confirm,
		constants.ActionCancel:            s.Cancel,
		constants.ActionRefund:            s.Refund,
	}, nil)
}
