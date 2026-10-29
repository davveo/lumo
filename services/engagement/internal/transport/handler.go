package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement/internal/usecase"
)

// New binds transport operations to independent business methods.
func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceEngagement, map[string]platform.Operation{
		constants.ActionSubscribe:     s.Subscribe,
		constants.ActionSubscriptions: s.Subscriptions,
		constants.ActionUnsubscribe:   s.Unsubscribe,
		constants.ActionPolicyGet:     s.PolicyGet,
		constants.ActionPolicyUpsert:  s.PolicyUpsert,
		constants.ActionAdminList:     s.AdminList,
		constants.ActionAdminCancel:   s.AdminCancel,
		constants.ActionList:          s.List,
		constants.ActionPut:           s.Put,
		constants.ActionDelete:        s.Delete,
		constants.ActionClear:         s.Clear,
	}, nil)
}
