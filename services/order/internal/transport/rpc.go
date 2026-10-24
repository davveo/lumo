package transport

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"time"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/usecase"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type orderServer struct {
	mallv1.UnimplementedOrderServiceServer
	service *usecase.Service
}

func Register(service *usecase.Service) func(grpc.ServiceRegistrar) {
	return func(registrar grpc.ServiceRegistrar) {
		mallv1.RegisterOrderServiceServer(registrar, &orderServer{service: service})
	}
}

func actor(value *mallv1.OrderActor) usecase.Actor {
	return usecase.Actor{UserID: value.GetUserId(), Role: value.GetRole()}
}
func request(value *mallv1.OrderRequest) usecase.OrderRequest {
	return usecase.OrderRequest{Actor: actor(value.GetActor()), ID: value.GetId()}
}

func rpcTime(value *timestamppb.Timestamp) (time.Time, error) {
	if value == nil {
		return time.Time{}, nil
	}
	if err := value.CheckValid(); err != nil {
		return time.Time{}, status.Error(codes.InvalidArgument, "invalid timestamp")
	}
	return value.AsTime(), nil
}

func orderMessage(order domain.Order) *mallv1.Order {
	return &mallv1.Order{Id: order.ID, UserId: order.UserID, Status: order.Status, Version: order.Version,
		PayableCents: order.Payable, RefundedTotalCents: order.Refunded, PaymentId: order.PaymentID, GroupId: order.GroupID,
		GroupFormed: order.GroupFormed, GroupFailed: order.GroupFailed, ResourcesCommitted: order.ResourcesCommitted,
		Note: order.Note, Source: order.Source, ExpiresAt: timestamppb.New(order.ExpiresAt),
		CreatedAt: timestamppb.New(order.CreatedAt), UpdatedAt: timestamppb.New(order.UpdatedAt),
		QuoteSnapshotJson: []byte(order.Quote), AddressSnapshotJson: []byte(order.Address)}
}

func callOrder(ctx context.Context, action, id string, input any, call func(context.Context) (domain.Order, error)) (traceResult *mallv1.Order, traceErr error) {
	ctx, span := platform.Start(ctx, "order.transport."+action)
	defer span.End()
	platform.TraceRequest(ctx, input)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if len(id) > 160 {
		return nil, status.Error(codes.InvalidArgument, "identifier too long")
	}
	order, err := call(ctx)
	if err != nil {
		return nil, err
	}
	return orderMessage(order), nil
}

func (s *orderServer) ListOrders(ctx context.Context, in *mallv1.ListOrdersRequest) (traceResult *mallv1.ListOrdersResponse, traceErr error) {
	ctx, span := platform.Start(ctx, "order.transport.list")
	defer span.End()
	platform.TraceRequest(ctx, in)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	orders, err := s.service.List(ctx, actor(in.GetActor()))
	if err != nil {
		return nil, err
	}
	items := make([]*mallv1.Order, 0, len(orders))
	for _, order := range orders {
		items = append(items, orderMessage(order))
	}
	return &mallv1.ListOrdersResponse{Items: items}, nil
}

func (s *orderServer) GetOrder(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionGet, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.Get(ctx, request(in)) })
}
func (s *orderServer) CreateOrder(ctx context.Context, in *mallv1.CreateOrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionCreate, in.GetId(), in, func(ctx context.Context) (domain.Order, error) {
		expiresAt, err := rpcTime(in.GetExpiresAt())
		if err != nil {
			return domain.Order{}, err
		}
		return s.service.Create(ctx, usecase.CreateOrderRequest{OrderRequest: usecase.OrderRequest{Actor: actor(in.GetActor()), ID: in.GetId()},
			Quote: in.GetQuoteSnapshotJson(), Address: in.GetAddressSnapshotJson(), Note: in.GetNote(), Source: in.GetSource(), ExpiresAt: expiresAt})
	})
}
func (s *orderServer) MarkPending(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionPending, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.Pending(ctx, request(in)) })
}
func (s *orderServer) MarkCreateFailed(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionCreateFailed, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.CreateFailed(ctx, request(in)) })
}
func (s *orderServer) MarkReady(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionReady, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.Ready(ctx, request(in)) })
}
func (s *orderServer) WaitForGroup(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionWaitGroup, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.WaitGroup(ctx, request(in)) })
}
func (s *orderServer) BeginClose(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionCloseBegin, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.CloseBegin(ctx, request(in)) })
}
func (s *orderServer) CompleteClose(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionCloseComplete, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.CloseComplete(ctx, request(in)) })
}
func (s *orderServer) MarkGroupFormed(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionGroupFormed, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.GroupFormed(ctx, request(in)) })
}
func (s *orderServer) MarkGroupFailed(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionGroupFailed, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.GroupFailed(ctx, request(in)) })
}
func (s *orderServer) MarkShipped(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionShipped, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.Shipped(ctx, request(in)) })
}
func (s *orderServer) CompleteOrder(ctx context.Context, in *mallv1.OrderRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionComplete, in.GetId(), in, func(ctx context.Context) (domain.Order, error) { return s.service.Complete(ctx, request(in)) })
}
func (s *orderServer) RecordRefund(ctx context.Context, in *mallv1.RecordRefundRequest) (*mallv1.Order, error) {
	return callOrder(ctx, constants.ActionRefunded, in.GetId(), in, func(ctx context.Context) (domain.Order, error) {
		return s.service.Refunded(ctx, usecase.RecordRefundRequest{OrderRequest: usecase.OrderRequest{Actor: actor(in.GetActor()), ID: in.GetId()}, RefundID: in.GetRefundId(), AmountCents: in.GetAmountCents()})
	})
}
func (s *orderServer) AcceptPayment(ctx context.Context, in *mallv1.AcceptPaymentRequest) (traceResult *mallv1.AcceptPaymentResponse, traceErr error) {
	ctx, span := platform.Start(ctx, "order.transport.accept_payment")
	defer span.End()
	platform.TraceRequest(ctx, in)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if len(in.GetId()) > 160 {
		return nil, status.Error(codes.InvalidArgument, "identifier too long")
	}
	paidAt, err := rpcTime(in.GetPaidAt())
	if err != nil {
		return nil, err
	}
	result, err := s.service.AcceptPayment(ctx, usecase.AcceptPaymentRequest{OrderRequest: usecase.OrderRequest{Actor: actor(in.GetActor()), ID: in.GetId()}, PaymentID: in.GetPaymentId(), AmountCents: in.GetAmountCents(), PaidAt: paidAt})
	if err != nil {
		return nil, err
	}
	return &mallv1.AcceptPaymentResponse{Accepted: result.Accepted, OrderId: result.OrderID, Status: result.Status, PaymentId: result.PaymentID}, nil
}
