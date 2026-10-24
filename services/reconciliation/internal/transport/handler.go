package transport

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/reconciliation/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func New(s *usecase.Service) *platform.MethodHandler {
	return platform.NewMethodHandler(constants.ServiceReconciliation, map[string]platform.Operation{
		constants.ActionList: s.List,
		constants.ActionRun:  s.Run,
	}, func(c platform.Request) error {
		if len(c.ID) > 160 || len(c.IdempotencyKey) > 160 {
			return status.Error(codes.InvalidArgument, "identifier too long")
		}
		return nil
	})
}
