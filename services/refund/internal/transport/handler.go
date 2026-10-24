package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/refund/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceRefund, map[string]platform.Operation{
		constants.ActionGet:         s.Get,
		constants.ActionList:        s.List,
		constants.ActionCreate:      s.Create,
		constants.ActionConfirmDemo: s.ConfirmDemo,
	}, func(c platform.Request) error {
		if len(c.ID) > 160 || len(c.IdempotencyKey) > 160 {
			return status.Error(codes.InvalidArgument, "identifier too long")
		}
		return nil
	})
}
