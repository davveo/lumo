package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServicePoints, map[string]platform.Operation{
		constants.ActionGet:     s.Get,
		constants.ActionCheckin: s.Checkin,
		constants.ActionReserve: s.Reserve,
		constants.ActionConfirm: s.Confirm,
		constants.ActionCancel:  s.Cancel,
		constants.ActionRefund:  s.Refund,
		constants.ActionReward:  s.Reward,
	}, nil)
}
