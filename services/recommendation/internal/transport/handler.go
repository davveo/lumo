package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/recommendation/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceRecommendation, map[string]platform.Operation{
		constants.ActionPolicyGet: s.PolicyGet, constants.ActionPolicyUpsert: s.PolicyUpsert,
		constants.ActionList:          s.List,
		constants.ActionRankBootstrap: s.RankBootstrap,
	}, nil)
}
