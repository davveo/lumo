package transport

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Executor interface {
	Execute(context.Context, platform.Command) (platform.Result, error)
}
type Handler struct{ App Executor }

func (h *Handler) Handle(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, s := platform.Start(ctx, "transport.inventory."+c.Action)
	defer s.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	switch c.Action {
	case constants.ActionGet,
		constants.ActionBatchGet,
		constants.ActionReserve,
		constants.ActionConfirm,
		constants.ActionCancel,
		constants.ActionDispatch,
		constants.ActionRestock,
		constants.ActionAdjust:
		return h.App.Execute(ctx, c)
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported inventory action")
	}
}
