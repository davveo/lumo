package transport

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/recommendation/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct{ s *usecase.Service }

func New(s *usecase.Service) *Handler { return &Handler{s: s} }
func (h *Handler) Handle(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "recommendation.transport."+c.Action)
	defer span.End()
	if c.Action == "" {
		return nil, status.Error(codes.InvalidArgument, "action required")
	}
	return h.s.Execute(ctx, c)
}
