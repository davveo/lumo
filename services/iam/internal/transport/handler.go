package transport

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/usecase"
)

type Handler struct{ s *usecase.Service }

func New(s *usecase.Service) *Handler { return &Handler{s: s} }
func (h *Handler) Handle(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "iam.transport."+c.Action)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	return h.s.Execute(ctx, c)
}
