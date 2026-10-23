package platform

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	otelcodes "go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

const rpcTokenHeader = "x-lumo-service-token"

type rpcCaller struct {
	targets     map[string]string
	token       string
	mu          sync.Mutex
	connections map[string]*grpc.ClientConn
}

func NewCaller(targets map[string]string, token string) Caller {
	copied := make(map[string]string, len(targets))
	for k, v := range targets {
		copied[k] = v
	}
	return &rpcCaller{targets: copied, token: token, connections: map[string]*grpc.ClientConn{}}
}
func (c *rpcCaller) connection(service string) (*grpc.ClientConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing := c.connections[service]; existing != nil {
		return existing, nil
	}
	target := c.targets[service]
	if target == "" {
		return nil, status.Errorf(codes.Unavailable, "unknown service %s", service)
	}
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(otelgrpc.NewClientHandler()), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4<<20)))
	if err != nil {
		return nil, err
	}
	c.connections[service] = conn
	return conn, nil
}
func (c *rpcCaller) Call(ctx context.Context, service string, command Command) (Result, error) {
	ctx, span := Start(ctx, "rpc.client."+service+"."+command.Action)
	defer span.End()
	if c.token == "" {
		return nil, status.Error(codes.Unauthenticated, "internal RPC credential is not configured")
	}
	conn, err := c.connection(service)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(command.Data)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid command payload")
	}
	// Child RPCs inherit an earlier parent deadline, bounding fan-out chains.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, rpcTokenHeader, c.token)
	result, err := mallv1.NewDomainServiceClient(conn).Execute(ctx, &mallv1.Command{Action: command.Action, Id: command.ID, UserId: command.UserID, Role: command.Role, IdempotencyKey: command.IdempotencyKey, PayloadJson: payload})
	if err != nil {
		span.SetStatus(otelcodes.Error, status.Code(err).String())
		return nil, err
	}
	var out Result
	decoder := json.NewDecoder(bytes.NewReader(result.GetPayloadJson()))
	decoder.UseNumber()
	if err = decoder.Decode(&out); err != nil {
		return nil, status.Error(codes.Internal, "invalid RPC response")
	}
	return out, nil
}
func (c *rpcCaller) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var result error
	for k, v := range c.connections {
		result = errors.Join(result, v.Close())
		delete(c.connections, k)
	}
	return result
}

type domainServer struct {
	mallv1.UnimplementedDomainServiceServer
	module Module
}

func (s *domainServer) Execute(ctx context.Context, in *mallv1.Command) (*mallv1.Result, error) {
	ctx, span := Start(ctx, "transport.grpc."+s.module.Name+"."+in.GetAction())
	defer span.End()
	if in.GetAction() == "" || len(in.GetAction()) > 80 {
		return nil, status.Error(codes.InvalidArgument, "invalid action")
	}
	var data map[string]any
	if len(in.GetPayloadJson()) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(in.GetPayloadJson()))
		decoder.UseNumber()
		if err := decoder.Decode(&data); err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid JSON payload")
		}
	}
	command := Command{Action: in.GetAction(), ID: in.GetId(), UserID: in.GetUserId(), Role: in.GetRole(), IdempotencyKey: in.GetIdempotencyKey(), Data: data}
	started := time.Now()
	result, err := s.module.Handle(ctx, command)
	if err != nil {
		code := status.Code(err)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			code = codes.NotFound
			err = status.Error(code, "record not found")
		} else if errors.Is(err, gorm.ErrDuplicatedKey) {
			code = codes.AlreadyExists
			err = status.Error(code, "record already exists")
		} else if code == codes.Unknown {
			code = codes.Internal
			err = status.Error(code, "operation failed")
		}
		span.SetStatus(otelcodes.Error, code.String())
		Log(ctx, slog.LevelWarn, "rpc operation failed", "action", command.Action, "code", code.String(), "duration_ms", time.Since(started).Milliseconds())
		return nil, err
	}
	b, err := json.Marshal(result)
	if err != nil {
		return nil, status.Error(codes.Internal, "response serialization failed")
	}
	Log(ctx, slog.LevelInfo, "rpc operation completed", "action", command.Action, "duration_ms", time.Since(started).Milliseconds())
	return &mallv1.Result{PayloadJson: b}, nil
}

// NewRPCServer is also usable with bufconn in integration tests. It exposes no
// reflection API, and all domain RPCs require the internal service credential.
func NewRPCServer(token string, module Module) (*grpc.Server, error) {
	if token == "" {
		return nil, fmt.Errorf("INTERNAL_RPC_TOKEN must not be empty")
	}
	if module.Handle == nil {
		return nil, fmt.Errorf("module %s has no RPC handler", module.Name)
	}
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod == "/grpc.health.v1.Health/Check" {
			return handler(ctx, req)
		}
		md, ok := metadata.FromIncomingContext(ctx)
		values := md.Get(rpcTokenHeader)
		if !ok || len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte(token)) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid internal service credential")
		}
		return handler(ctx, req)
	}
	server := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()), grpc.UnaryInterceptor(interceptor), grpc.MaxRecvMsgSize(4<<20), grpc.MaxSendMsgSize(4<<20))
	mallv1.RegisterDomainServiceServer(server, &domainServer{module: module})
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
	healthv1.RegisterHealthServer(server, healthServer)
	return server, nil
}
func ServeRPC(ctx context.Context, addr, token string, module Module) error {
	server, err := NewRPCServer(token, module)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen RPC: %w", err)
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			stopped := make(chan struct{})
			go func() { server.GracefulStop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(10 * time.Second):
				server.Stop()
			}
		case <-done:
		}
	}()
	err = server.Serve(listener)
	if errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}
	return err
}
