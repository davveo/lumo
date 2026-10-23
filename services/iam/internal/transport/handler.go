package transport

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/usecase"
)

type Handler struct{ s *usecase.Service }

func New(s *usecase.Service) *Handler { return &Handler{s: s} }
func (h *Handler) Handle(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "iam.transport."+c.Action)
	defer span.End()
	return h.s.Execute(ctx, c)
}
