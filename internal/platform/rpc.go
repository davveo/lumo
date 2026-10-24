package platform

import (
	"github.com/davveo/lumo/internal/constants"

	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

const rpcTokenHeader = "x-lumo-service-token"

type rpcCaller struct {
	targets     map[string]string
	token       string
	mu          sync.Mutex
	connections map[string]*grpc.ClientConn
	consul      *consulResolverBuilder
	closed      bool
}

func NewCaller(targets map[string]string, token string) Caller {
	return NewCallerWithConsul(targets, token, nil)
}

// NewCallerWithConsul resolves consul:///service targets through healthy Consul
// instances. Explicit host:port targets remain independent of Consul.
func NewCallerWithConsul(targets map[string]string, token string, consul *Consul) Caller {
	copied := make(map[string]string, len(targets))
	for k, v := range targets {
		copied[k] = v
	}
	caller := &rpcCaller{targets: copied, token: token, connections: map[string]*grpc.ClientConn{}}
	if consul != nil {
		caller.consul = &consulResolverBuilder{client: consul, pollInterval: 2 * time.Second}
	}
	return caller
}
func (c *rpcCaller) connection(service string) (*grpc.ClientConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, status.Error(codes.Unavailable, "RPC caller is closed")
	}
	if existing := c.connections[service]; existing != nil {
		return existing, nil
	}
	target := c.targets[service]
	if target == "" {
		return nil, status.Errorf(codes.Unavailable, "unknown service %s", service)
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(rpcPayloadClientInterceptor), grpc.WithStatsHandler(&rpcPayloadStatsHandler{client: true, Handler: otelgrpc.NewClientHandler(otelgrpc.WithSpanAttributes(
		attribute.String("peer.service", service), attribute.String("lumo.target_service", service),
	))}), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4 << 20))}
	if strings.HasPrefix(target, consulResolverScheme+":") {
		if c.consul == nil {
			return nil, status.Error(codes.Unavailable, "Consul discovery is not configured")
		}
		options = append(options, grpc.WithResolvers(c.consul), grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`))
	}
	// NewClient does not perform resolution or network I/O; resolution starts
	// lazily when the first RPC leaves the connection map lock.
	conn, err := grpc.NewClient(target, options...)
	if err != nil {
		return nil, err
	}
	c.connections[service] = conn
	return conn, nil
}
func (c *rpcCaller) Call(ctx context.Context, service string, command Command) (out Result, err error) {
	action := rpcTraceAction(command.Action)
	ctx, span := Start(ctx, "rpc.client."+service+"."+action)
	span.SetAttributes(attribute.String("lumo.target_service", service), attribute.String("lumo.action", action))
	defer span.End()
	TraceRequest(ctx, command)
	defer func() {
		TraceResponse(ctx, out, err)
		if err != nil {
			span.SetStatus(otelcodes.Error, status.Code(err).String())
		}
	}()
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
	if service == constants.ServiceOrder {
		out, handled, typedErr := callTypedOrder(ctx, conn, command)
		// A rolling upgrade may reach an order instance that only has Execute.
		// Business errors never retry through a different transport method.
		if handled && !missingOrderRPC(typedErr) {
			if typedErr != nil {
				span.SetStatus(otelcodes.Error, status.Code(typedErr).String())
			}
			return out, typedErr
		}
	}
	result, err := mallv1.NewDomainServiceClient(conn).Execute(ctx, &mallv1.Command{Action: command.Action, Id: command.ID, UserId: command.UserID, Role: command.Role, IdempotencyKey: command.IdempotencyKey, PayloadJson: payload})
	if err != nil {
		span.SetStatus(otelcodes.Error, status.Code(err).String())
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(result.GetPayloadJson()))
	decoder.UseNumber()
	if err = decoder.Decode(&out); err != nil {
		return nil, status.Error(codes.Internal, "invalid RPC response")
	}
	return out, nil
}
func (c *rpcCaller) Close() error {
	c.mu.Lock()
	c.closed = true
	connections := c.connections
	c.connections = nil
	c.mu.Unlock()
	var result error
	for _, v := range connections {
		result = errors.Join(result, v.Close())
	}
	return result
}

type domainServer struct {
	mallv1.UnimplementedDomainServiceServer
	module Module
}

func rpcError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return status.Error(codes.NotFound, "record not found")
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return status.Error(codes.AlreadyExists, "record already exists")
	}
	if err != nil && status.Code(err) == codes.Unknown {
		return status.Error(codes.Internal, "operation failed")
	}
	return err
}

// Invalid command actions should not turn span names into arbitrary-size input.
func rpcTraceAction(action string) string {
	if action == "" || len(action) > 80 {
		return "unknown"
	}
	return action
}

func (s *domainServer) Execute(ctx context.Context, in *mallv1.Command) (out *mallv1.Result, err error) {
	action := rpcTraceAction(in.GetAction())
	ctx, span := Start(ctx, "transport.grpc."+s.module.Name+"."+action)
	span.SetAttributes(attribute.String("lumo.target_service", s.module.Name), attribute.String("lumo.action", action))
	defer span.End()
	TraceRequest(ctx, in)
	defer func() { TraceResponse(ctx, out, err) }()
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
		err = rpcError(err)
		code := status.Code(err)
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
	if module.Handle == nil && module.RegisterRPC == nil {
		return nil, fmt.Errorf("module %s has no RPC handler", module.Name)
	}
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod == "/grpc.health.v1.Health/Check" {
			return handler(ctx, req)
		}
		// This span was created by the otelgrpc stats handler. Its rpc.service
		// remains the protobuf service; the module identifies the business target.
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("lumo.target_service", module.Name))
		md, ok := metadata.FromIncomingContext(ctx)
		values := md.Get(rpcTokenHeader)
		if !ok || len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte(token)) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid internal service credential")
		}
		if info.FullMethod == mallv1.DomainService_Execute_FullMethodName {
			if command, ok := req.(*mallv1.Command); ok {
				trace.SpanFromContext(ctx).SetAttributes(attribute.String("lumo.action", rpcTraceAction(command.GetAction())))
			}
		}
		if action := module.RPCActions[info.FullMethod]; action != "" {
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("lumo.action", action))
			ctx, span := Start(ctx, "transport.grpc."+module.Name+"."+action)
			span.SetAttributes(attribute.String("lumo.target_service", module.Name), attribute.String("lumo.action", action))
			defer span.End()
			TraceRequest(ctx, req)
			started := time.Now()
			result, err := handler(ctx, req)
			err = rpcError(err)
			TraceResponse(ctx, result, err)
			level, message := slog.LevelInfo, "rpc operation completed"
			if err != nil {
				level, message = slog.LevelWarn, "rpc operation failed"
				span.SetStatus(otelcodes.Error, status.Code(err).String())
			}
			Log(ctx, level, message, "action", action, "code", status.Code(err).String(), "duration_ms", time.Since(started).Milliseconds())
			return result, err
		}
		return handler(ctx, req)
	}
	server := grpc.NewServer(grpc.StatsHandler(&rpcPayloadStatsHandler{Handler: otelgrpc.NewServerHandler()}), grpc.UnaryInterceptor(interceptor), grpc.MaxRecvMsgSize(4<<20), grpc.MaxSendMsgSize(4<<20))
	if module.Handle != nil {
		mallv1.RegisterDomainServiceServer(server, &domainServer{module: module})
	}
	if module.RegisterRPC != nil {
		module.RegisterRPC(server)
	}
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
	for name := range server.GetServiceInfo() {
		healthServer.SetServingStatus(name, healthv1.HealthCheckResponse_SERVING)
	}
	healthv1.RegisterHealthServer(server, healthServer)
	return server, nil
}

type rpcTraceRequestKey struct{}
type rpcTraceRequest struct{ value any }

// Client interceptors run before the stats handler creates the automatic gRPC
// span. Carry only the message, never metadata, into that handler's context.
func rpcPayloadClientInterceptor(ctx context.Context, method string, request, reply any, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
	ctx = context.WithValue(ctx, rpcTraceRequestKey{}, rpcTraceRequest{value: request})
	return invoke(ctx, method, request, reply, connection, options...)
}

// The wrapped OTel handler owns the automatic span lifecycle. Message stats are
// recorded on that same span; errors must be added before stats.End closes it.
type rpcPayloadStatsHandler struct {
	stats.Handler
	client bool
}

func (h *rpcPayloadStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	ctx = h.Handler.TagRPC(ctx, info)
	if h.client {
		if request, ok := ctx.Value(rpcTraceRequestKey{}).(rpcTraceRequest); ok {
			TraceRequest(ctx, request.value)
		}
	}
	return ctx
}

func (h *rpcPayloadStatsHandler) HandleRPC(ctx context.Context, event stats.RPCStats) {
	switch message := event.(type) {
	case *stats.InPayload:
		if h.client {
			TraceResponse(ctx, message.Payload, nil)
		} else {
			TraceRequest(ctx, message.Payload)
		}
	case *stats.OutPayload:
		if !h.client {
			TraceResponse(ctx, message.Payload, nil)
		}
	case *stats.End:
		if message.Error != nil {
			TraceResponse(ctx, nil, message.Error)
			// OTel otherwise copies the raw gRPC status message into the span's
			// status description. Keep its code and metrics while preventing an
			// error that embeds request data from bypassing payload redaction.
			code := status.Code(message.Error)
			if errors.Is(message.Error, context.Canceled) {
				code = codes.Canceled
			} else if errors.Is(message.Error, context.DeadlineExceeded) {
				code = codes.DeadlineExceeded
			}
			safe := *message
			safe.Error = status.Error(code, code.String())
			event = &safe
		}
	}
	h.Handler.HandleRPC(ctx, event)
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
