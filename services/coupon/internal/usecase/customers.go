package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) CustomerAssets(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "coupon.usecase.CustomerAssets")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if c.ID == "" || len(c.ID) > 128 {
		return nil, status.Error(codes.InvalidArgument, "customer id required")
	}
	return s.Repo.CustomerAssets(ctx, c.ID)
}
