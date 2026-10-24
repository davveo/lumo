package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceAftersale, map[string]platform.Operation{
		constants.ActionCreate:   s.Create,
		constants.ActionGet:      s.Get,
		constants.ActionList:     s.List,
		constants.ActionApprove:  s.Approve,
		constants.ActionReject:   s.Reject,
		constants.ActionCancel:   s.Cancel,
		constants.ActionReturn:   s.Return,
		constants.ActionInspect:  s.Inspect,
		constants.ActionRefund:   s.Refund,
		constants.ActionExchange: s.Exchange,
	}, nil)
}
