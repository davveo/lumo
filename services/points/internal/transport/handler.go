package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/usecase"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServicePoints, map[string]platform.Operation{
		constants.ActionOffers:           s.Offers,
		constants.ActionOffersAdmin:      s.OffersAdmin,
		constants.ActionOfferUpsert:      s.OfferUpsert,
		constants.ActionRedeem:           s.Redeem,
		constants.ActionRedemptions:      s.Redemptions,
		constants.ActionRedemptionsAdmin: s.RedemptionsAdmin,
		constants.ActionRedemptionGet:    s.RedemptionGet,
		constants.ActionRedemptionRetry:  s.RedemptionRetry,
		constants.ActionPolicyGet:        s.PolicyGet,
		constants.ActionPolicyUpsert:     s.PolicyUpsert,
		constants.ActionAdminGet:         s.AdminGet,
		constants.ActionAdjust:           s.Adjust,
		constants.ActionLedger:           s.Ledger,
		constants.ActionMyLedger:         s.MyLedger,

		constants.ActionGet:     s.Get,
		constants.ActionCheckin: s.Checkin,
		constants.ActionReserve: s.Reserve,
		constants.ActionConfirm: s.Confirm,
		constants.ActionCancel:  s.Cancel,
		constants.ActionRefund:  s.Refund,
		constants.ActionReward:  s.Reward,
	}, nil)
}
