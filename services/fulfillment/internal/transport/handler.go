package transport

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/fulfillment/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct{ Service *usecase.Service }

func (h *Handler) Handle(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, s := platform.Start(ctx, "transport.fulfillment."+c.Action)
	defer s.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	switch c.Action {
	case "list", "get", "dispatch", "cancel", "inspect_return", "create_exchange":
		return h.Service.Handle(ctx, c)
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported fulfillment action")
	}
}
