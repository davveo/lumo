package transport

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/checkout/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func New(u *usecase.Service) func(context.Context, platform.Command) (platform.Result, error) {
	return func(ctx context.Context, c platform.Command) (platform.Result, error) {
		ctx, s := platform.Start(ctx, "checkout.transport."+c.Action)
		defer s.End()
		if len(c.ID) > 160 || len(c.IdempotencyKey) > 160 {
			return nil, status.Error(codes.InvalidArgument, "identifier too long")
		}
		return u.Handle(ctx, c)
	}
}
