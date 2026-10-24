package platform

import (
	"context"
	"github.com/davveo/lumo/internal/constants"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func methodTransportSpan(service, action string) string {
	switch service {
	case constants.ServiceAftersale, constants.ServiceCampaign, constants.ServiceCoupon,
		constants.ServiceFlash, constants.ServiceFulfillment, constants.ServiceGroup,
		constants.ServiceInventory, constants.ServiceInvoice, constants.ServiceLogistics, constants.ServicePoints:
		return "transport." + service + "." + action
	default:
		return service + ".transport." + action
	}
}

// MethodHandler is the legacy HTTP/Execute adapter. Business implementations
// receive an action-free Request through a separately registered method.
type MethodHandler struct {
	operations map[string]Operation
}

func NewMethodHandler(
	service string,
	operations map[string]Operation,
	validate func(Request) error,
) *MethodHandler {
	h := &MethodHandler{operations: make(map[string]Operation, len(operations))}
	for action, operation := range operations {
		h.operations[action] = func(ctx context.Context, request Request) (result Result, err error) {
			ctx, span := Start(ctx, methodTransportSpan(service, action))
			defer span.End()
			TraceRequest(ctx, request)
			defer func() { TraceResponse(ctx, result, err) }()
			if validate != nil {
				if err := validate(request); err != nil {
					return nil, err
				}
			}
			return operation(ctx, request)
		}
	}
	return h
}

// Operations returns a copy so callers cannot mutate another adapter's routes.
func (h *MethodHandler) Operations() map[string]Operation {
	operations := make(map[string]Operation, len(h.operations))
	for action, operation := range h.operations {
		operations[action] = operation
	}
	return operations
}

func (h *MethodHandler) Handle(ctx context.Context, command Command) (Result, error) {
	if command.Action == "" {
		return nil, status.Error(codes.InvalidArgument, "action required")
	}
	operation, ok := h.operations[command.Action]
	if !ok {
		return nil, status.Error(codes.Unimplemented, "unsupported operation")
	}
	return operation(ctx, command.Request())
}
