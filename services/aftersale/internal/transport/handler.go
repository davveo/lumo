package transport

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct{ Service *usecase.Service }

func (h *Handler) Handle(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "transport.aftersale."+c.Action)
	defer s.End()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	switch c.Action {
	case "create", "get", "list", "approve", "reject", "cancel", "return", "inspect", "refund", "exchange":
		return h.Service.Handle(ctx, c)
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported aftersale action")
	}
}
