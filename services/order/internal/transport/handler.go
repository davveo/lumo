package transport

import (
	"context"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type legacyAction func(context.Context, platform.Command) (platform.Result, error)
type legacyAdapter struct{ service *usecase.Service }

// New keeps legacy Execute routing at the transport boundary. Each entry adapts
// to an independent typed usecase; this table contains no business rules.
func New(service *usecase.Service) func(context.Context, platform.Command) (platform.Result, error) {
	adapter := legacyAdapter{service: service}
	actions := map[string]legacyAction{
		"list":           adapter.list,
		"get":            legacyOrder(service.Get),
		"create":         adapter.create,
		"pending":        legacyOrder(service.Pending),
		"create_failed":  legacyOrder(service.CreateFailed),
		"accept_payment": adapter.acceptPayment,
		"ready":          legacyOrder(service.Ready),
		"wait_group":     legacyOrder(service.WaitGroup),
		"close_begin":    legacyOrder(service.CloseBegin),
		"close_complete": legacyOrder(service.CloseComplete),
		"group_formed":   legacyOrder(service.GroupFormed),
		"group_failed":   legacyOrder(service.GroupFailed),
		"shipped":        legacyOrder(service.Shipped),
		"complete":       legacyOrder(service.Complete),
		"refunded":       adapter.refunded,
	}
	return func(ctx context.Context, command platform.Command) (traceResult platform.Result, traceErr error) {
		ctx, span := platform.Start(ctx, "order.transport."+command.Action)
		defer span.End()
		platform.TraceRequest(ctx, command)
		defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
		if len(command.ID) > 160 || len(command.IdempotencyKey) > 160 {
			return nil, status.Error(codes.InvalidArgument, "identifier too long")
		}
		if err := platform.RequireUser(command); err != nil {
			return nil, err
		}
		call, ok := actions[command.Action]
		if !ok {
			return nil, status.Error(codes.Unimplemented, "unsupported order action")
		}
		return call(ctx, command)
	}
}
