package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) IssueReward(ctx context.Context, c platform.Request) (platform.Result, error) {
	if c.Role != constants.RoleSystem || c.UserID == "" {
		return nil, status.Error(codes.PermissionDenied, "internal reward issuer required")
	}
	if !platform.IsBusinessID(c.ID, "redemption") || platform.Text(c.Data, "template_id") == "" {
		return nil, status.Error(codes.InvalidArgument, "redemption and template required")
	}
	return s.Repo.IssueReward(ctx, c)
}
