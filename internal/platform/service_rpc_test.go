package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"github.com/davveo/lumo/internal/constants"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func methodTestConnection(t *testing.T, module Module) *grpc.ClientConn {
	t.Helper()
	server, err := NewRPCServer("named-test-token", module)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(4 << 20)
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.NewClient(
		"passthrough:///named-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close(); server.Stop(); _ = listener.Close() })
	return connection
}

func methodTestCaller(connection *grpc.ClientConn, service string) *rpcCaller {
	return &rpcCaller{token: "named-test-token", targets: map[string]string{},
		connections: map[string]*grpc.ClientConn{service: connection}}
}

func TestEveryGeneratedDomainMethodUsesItsOwnRPC(t *testing.T) {
	for service, definitions := range namedServiceRPCs {
		t.Run(service, func(t *testing.T) {
			var legacy atomic.Int64
			operations := map[string]Operation{}
			for action := range definitions {
				operations[action] = func(_ context.Context, request Request) (Result, error) {
					if request.UserID != "buyer" || request.Role != constants.RoleUser || request.ID != "record" ||
						request.IdempotencyKey != "stable-key" || Int(request.Data, "amount") != 9007199254741115 {
						t.Errorf("identity, metadata or integer precision changed: %+v", request)
					}
					return Result{"selected": action, "amount": request.Data["amount"]}, nil
				}
			}
			connection := methodTestConnection(t, Module{Name: service, Operations: operations,
				Handle: func(context.Context, Command) (Result, error) {
					legacy.Add(1)
					return nil, status.Error(codes.Internal, "legacy dispatcher must not run")
				}})
			caller := methodTestCaller(connection, service)
			for method, action := range namedOperationActions[service] {
				result, err := caller.Invoke(context.Background(), service, method, Request{
					ID: "record", UserID: "buyer", Role: constants.RoleUser, IdempotencyKey: "stable-key",
					Data: map[string]any{"amount": json.Number("9007199254741115"), "action": "forged-operation"},
				})
				if err != nil || Text(result, "selected") != action || Int(result, "amount") != 9007199254741115 {
					t.Fatalf("%s.%s did not invoke its declared method: %v %v", service, method, result, err)
				}
			}
			if legacy.Load() != 0 {
				t.Fatal("named methods fell back to Execute")
			}
		})
	}
}

func TestNamedRPCValidatesCredentialPayloadAndPreservesErrors(t *testing.T) {
	var calls, legacy atomic.Int64
	operation := func(ctx context.Context, request Request) (Result, error) {
		calls.Add(1)
		switch Text(request.Data, "error") {
		case "business":
			return nil, status.Error(codes.Unimplemented, "business policy is unavailable")
		case "permission":
			return nil, status.Error(codes.PermissionDenied, "not allowed")
		case "internal":
			return nil, errors.New("private database details")
		case "deadline":
			<-ctx.Done()
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return Result{"ok": true}, nil
	}
	connection := methodTestConnection(t, Module{Name: constants.ServiceCatalog,
		Operations: map[string]Operation{constants.ActionGet: operation},
		Handle:     func(context.Context, Command) (Result, error) { legacy.Add(1); return Result{}, nil },
	})
	wire := mallv1.NewCatalogServiceClient(connection)
	if _, err := wire.Get(context.Background(), &mallv1.ServiceRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing internal credential accepted: %v", err)
	}
	authorized := metadata.AppendToOutgoingContext(context.Background(), rpcTokenHeader, "named-test-token")
	for _, payload := range []string{`{`, `[]`, `{} {}`, `null {}`} {
		if _, err := wire.Get(authorized, &mallv1.ServiceRequest{PayloadJson: []byte(payload)}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid JSON %q reached method: %v", payload, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected wire input reached the business operation")
	}
	caller := methodTestCaller(connection, constants.ServiceCatalog)
	for input, code := range map[string]codes.Code{"business": codes.Unimplemented, "permission": codes.PermissionDenied, "internal": codes.Internal} {
		_, err := NewClients(caller).Catalog.Get(context.Background(), Request{Data: map[string]any{"error": input}})
		if status.Code(err) != code {
			t.Errorf("%s changed error or replayed: %v", input, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := NewClients(caller).Catalog.Get(ctx, Request{Data: map[string]any{"error": "deadline"}})
	if status.Code(err) != codes.DeadlineExceeded || legacy.Load() != 0 {
		t.Fatalf("deadline was replayed or changed: %v, legacy=%d", err, legacy.Load())
	}
	if _, err := caller.Invoke(context.Background(), constants.ServiceCatalog, "Missing", Request{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("unknown named method accepted: %v", err)
	}
}

func TestNamedRPCFallsBackOnlyForAnOlderExecuteOnlyServer(t *testing.T) {
	var calls atomic.Int64
	connection := methodTestConnection(t, Module{Name: constants.ServiceCatalog,
		Handle: func(_ context.Context, command Command) (Result, error) {
			calls.Add(1)
			return Result{"operation": command.Action, "id": command.ID}, nil
		}})
	result, err := NewClients(methodTestCaller(connection, constants.ServiceCatalog)).Catalog.Get(
		context.Background(), Request{ID: "legacy-record"},
	)
	if err != nil || Text(result, "operation") != constants.ActionGet || Text(result, "id") != "legacy-record" || calls.Load() != 1 {
		t.Fatalf("rolling upgrade compatibility failed: %v %v calls=%d", result, err, calls.Load())
	}
}
