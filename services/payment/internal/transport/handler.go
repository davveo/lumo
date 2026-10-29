package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/payment/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServicePayment, map[string]platform.Operation{constants.ActionMethods: s.Methods, constants.ActionPolicyGet: s.PolicyGet, constants.ActionPolicyUpsert: s.PolicyUpsert,
		constants.ActionAdminList:    s.AdminList,
		constants.ActionAdminSummary: s.AdminSummary,
		constants.ActionGet:          s.Get,
		constants.ActionList:         s.List,
		constants.ActionCreate:       s.Create,
		constants.ActionSubmitDemo:   s.SubmitDemo,
		constants.ActionConfirmDemo:  s.ConfirmDemo,
		constants.ActionClose:        s.Close,
	}, func(c platform.Request) error {
		if len(c.ID) > 160 || len(c.IdempotencyKey) > 160 {
			return status.Error(codes.InvalidArgument, "identifier too long")
		}
		return nil
	})
}
