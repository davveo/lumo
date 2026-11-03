package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) ReminderGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	return s.Repo.ReminderGet(ctx, c.UserID)
}
func (s *Service) ReminderUpdate(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	enabled, ok := c.Data["enabled"].(bool)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "enabled boolean required")
	}
	return s.Repo.ReminderUpdate(ctx, c.UserID, enabled)
}
func (s *Service) Tick(ctx context.Context) error { return s.Repo.QueueExpiryReminders(ctx) }
