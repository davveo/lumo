package platform

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"encoding/json"
	"strings"
	"time"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"github.com/davveo/lumo/internal/orderwire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type orderRPC func(context.Context, mallv1.OrderServiceClient, Command) (Result, error)

// This is a legacy caller adapter. New clients can use OrderServiceClient
// directly; usecases never receive an action string or a platform.Command.
var orderRPCs = map[string]orderRPC{
	constants.ActionList: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		response, err := client.ListOrders(ctx, &mallv1.ListOrdersRequest{Actor: orderActor(c)})
		if err != nil {
			return nil, err
		}
		return orderwire.ListResult(response), nil
	},
	constants.ActionGet: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		return orderResult(client.GetOrder(ctx, orderRequest(c)))
	},
	constants.ActionCreate: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		quote, _ := json.Marshal(c.Data["quote"])
		address, _ := json.Marshal(c.Data["address"])
		return orderResult(client.CreateOrder(ctx, &mallv1.CreateOrderRequest{Actor: orderActor(c), Id: c.ID,
			QuoteSnapshotJson: quote, AddressSnapshotJson: address, Note: Text(c.Data, "note"),
			Source: Text(c.Data, "source"), ExpiresAt: orderTime(Text(c.Data, "expires_at"))}))
	},
	constants.ActionPending: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		return orderResult(client.MarkPending(ctx, orderRequest(c)))
	},
	constants.ActionCreateFailed: func(
		ctx context.Context,
		client mallv1.OrderServiceClient,
		c Command,
	) (
		Result,
		error,
	) {
		return orderResult(client.MarkCreateFailed(ctx, orderRequest(c)))
	},
	constants.ActionAcceptPayment: func(
		ctx context.Context,
		client mallv1.OrderServiceClient,
		c Command,
	) (
		Result,
		error,
	) {
		response, err := client.AcceptPayment(ctx, &mallv1.AcceptPaymentRequest{Actor: orderActor(c), Id: c.ID,
			PaymentId: Text(
				c.Data,
				"payment_id",
			), AmountCents: Int(c.Data, "amount_cents"), PaidAt: orderTime(Text(c.Data, "paid_at"))})
		if err != nil {
			return nil, err
		}
		return orderwire.PaymentResult(response), nil
	},
	constants.ActionReady: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		return orderResult(client.MarkReady(ctx, orderRequest(c)))
	},
	constants.ActionWaitGroup: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		return orderResult(client.WaitForGroup(ctx, orderRequest(c)))
	},
	constants.ActionCloseBegin: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		return orderResult(client.BeginClose(ctx, orderRequest(c)))
	},
	constants.ActionCloseComplete: func(
		ctx context.Context,
		client mallv1.OrderServiceClient,
		c Command,
	) (
		Result,
		error,
	) {
		return orderResult(client.CompleteClose(ctx, orderRequest(c)))
	},
	constants.ActionGroupFormed: func(
		ctx context.Context,
		client mallv1.OrderServiceClient,
		c Command,
	) (
		Result,
		error,
	) {
		return orderResult(client.MarkGroupFormed(ctx, orderRequest(c)))
	},
	constants.ActionGroupFailed: func(
		ctx context.Context,
		client mallv1.OrderServiceClient,
		c Command,
	) (
		Result,
		error,
	) {
		return orderResult(client.MarkGroupFailed(ctx, orderRequest(c)))
	},
	constants.ActionShipped: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		return orderResult(client.MarkShipped(ctx, orderRequest(c)))
	},
	constants.ActionComplete: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		return orderResult(client.CompleteOrder(ctx, orderRequest(c)))
	},
	constants.ActionRefunded: func(ctx context.Context, client mallv1.OrderServiceClient, c Command) (Result, error) {
		return orderResult(client.RecordRefund(ctx, &mallv1.RecordRefundRequest{Actor: orderActor(c), Id: c.ID,
			RefundId: Text(c.Data, "refund_id"), AmountCents: Int(c.Data, "amount_cents")}))
	},
}

func callTypedOrder(ctx context.Context, connection grpc.ClientConnInterface, command Command) (Result, bool, error) {
	call, ok := orderRPCs[command.Action]
	if !ok {
		return nil, false, nil
	}
	if len(command.ID) > 160 || len(command.IdempotencyKey) > 160 {
		return nil, true, status.Error(codes.InvalidArgument, "identifier too long")
	}
	result, err := call(ctx, mallv1.NewOrderServiceClient(connection), command)
	return result, true, err
}

func missingOrderRPC(err error) bool {
	if status.Code(err) != codes.Unimplemented {
		return false
	}
	message := status.Convert(err).Message()
	if message == "unknown service mall.v1.OrderService" {
		return true
	}
	for method := range orderwire.Actions() {
		name := method[strings.LastIndex(method, "/")+1:]
		if message == "method "+name+" not implemented" ||
			message == "unknown method "+name+" for service mall.v1.OrderService" {
			return true
		}
	}
	return false
}

func orderActor(command Command) *mallv1.OrderActor {
	return &mallv1.OrderActor{UserId: command.UserID, Role: command.Role}
}

func orderRequest(command Command) *mallv1.OrderRequest {
	return &mallv1.OrderRequest{Actor: orderActor(command), Id: command.ID}
}

func orderTime(raw string) *timestamppb.Timestamp {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil
	}
	return timestamppb.New(value)
}

func orderResult(order *mallv1.Order, err error) (Result, error) {
	if err != nil {
		return nil, err
	}
	return orderwire.Result(order), nil
}
