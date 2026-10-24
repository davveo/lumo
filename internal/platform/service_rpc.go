package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

//go:generate python3 ../../scripts/generate-service-rpc.py

// MethodCaller invokes a named domain method. Caller remains only as the
// compatibility boundary for existing HTTP adapters and in-process test peers.
type MethodCaller interface {
	Invoke(context.Context, string, string, Request) (Result, error)
}

func invokeMethod(
	ctx context.Context,
	caller Caller,
	service, method, action string,
	request Request,
) (Result, error) {
	if caller == nil {
		return nil, status.Error(codes.Unavailable, "RPC peer unavailable")
	}
	if client, ok := caller.(MethodCaller); ok {
		return client.Invoke(ctx, service, method, request)
	}
	// Custom local callers may implement the pre-migration interface. Production
	// rpcCaller always implements MethodCaller and uses the generated domain RPC.
	return caller.Call(ctx, service, request.Command(action))
}

func (c *rpcCaller) Invoke(ctx context.Context, service, method string, request Request) (Result, error) {
	action, ok := namedOperationActions[service][method]
	if !ok {
		return nil, status.Error(codes.Unimplemented, "unknown domain method")
	}
	return c.Call(ctx, service, request.Command(action))
}

func callServiceMethod(
	ctx context.Context,
	connection grpc.ClientConnInterface,
	service string,
	command Command,
	payload []byte,
) (Result, bool, error) {
	definition, ok := namedServiceRPCs[service][command.Action]
	if !ok {
		return nil, false, nil
	}
	response, err := definition.invoke(ctx, connection, &mallv1.ServiceRequest{
		Id: command.ID, UserId: command.UserID, Role: command.Role,
		IdempotencyKey: command.IdempotencyKey, PayloadJson: payload,
	})
	if missingServiceMethod(err, definition.fullMethod) {
		return nil, false, nil
	}
	result, err := decodeServiceResponse(response, err)
	return result, true, err
}

func serveMethod(
	ctx context.Context,
	operation Operation,
	wire *mallv1.ServiceRequest,
) (*mallv1.ServiceResponse, error) {
	if operation == nil {
		return nil, status.Error(codes.Unimplemented, "domain method is not registered")
	}
	if wire == nil {
		return nil, status.Error(codes.InvalidArgument, "request required")
	}
	var data map[string]any
	if len(wire.GetPayloadJson()) != 0 {
		decoder := json.NewDecoder(bytes.NewReader(wire.GetPayloadJson()))
		decoder.UseNumber()
		if err := decoder.Decode(&data); err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid JSON payload")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, status.Error(codes.InvalidArgument, "invalid trailing JSON payload")
		}
	}
	result, err := operation(ctx, Request{
		ID: wire.GetId(), UserID: wire.GetUserId(), Role: wire.GetRole(),
		IdempotencyKey: wire.GetIdempotencyKey(), Data: data,
	})
	if err != nil {
		return nil, rpcError(err)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, status.Error(codes.Internal, "response serialization failed")
	}
	return &mallv1.ServiceResponse{PayloadJson: payload}, nil
}

func decodeServiceResponse(response *mallv1.ServiceResponse, err error) (Result, error) {
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, status.Error(codes.Internal, "missing RPC response")
	}
	var result Result
	decoder := json.NewDecoder(bytes.NewReader(response.GetPayloadJson()))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, status.Error(codes.Internal, "invalid RPC response")
	}
	return result, nil
}

// Only an explicitly absent method/service permits rolling-upgrade fallback.
// Application Unimplemented, permission, deadline and network errors never replay.
func missingServiceMethod(err error, fullMethod string) bool {
	if status.Code(err) != codes.Unimplemented {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(fullMethod, "/"), "/")
	if len(parts) != 2 {
		return false
	}
	message := status.Convert(err).Message()
	return message == "unknown service "+parts[0] ||
		message == "unknown method "+parts[1]+" for service "+parts[0] ||
		message == "method "+parts[1]+" not implemented"
}
