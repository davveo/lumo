package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/member/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceMember, map[string]platform.Operation{
		constants.ActionPolicyGet: s.PolicyGet, constants.ActionPolicyUpsert: s.PolicyUpsert,
		constants.ActionGet:    s.Get,
		constants.ActionUpdate: s.Update,
	}, nil)
}
