// Package orderwire contains the compatibility mapping between the typed order
// RPC contract and the existing JSON gateway. It contains no business rules.
package orderwire

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func snapshot(raw []byte) map[string]any {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	_ = decoder.Decode(&value)
	return value
}

func timestamp(value *timestamppb.Timestamp) string {
	if value == nil {
		return time.Time{}.Format(time.RFC3339Nano)
	}
	return value.AsTime().Format(time.RFC3339Nano)
}

func number(value int64) json.Number { return json.Number(strconv.FormatInt(value, 10)) }

// Result preserves the public JSON order shape while the RPC itself returns
// explicit protobuf fields. Quote/address are immutable historical documents.
func Result(order *mallv1.Order) map[string]any {
	quote, address := snapshot(order.GetQuoteSnapshotJson()), snapshot(order.GetAddressSnapshotJson())
	result := map[string]any{
		"id": order.GetId(), "user_id": order.GetUserId(), "status": order.GetStatus(),
		"version": number(order.GetVersion()), "payable_cents": number(order.GetPayableCents()),
		"refunded_total_cents": number(order.GetRefundedTotalCents()),
		"source":               order.GetSource(), "payment_id": order.GetPaymentId(), "group_id": order.GetGroupId(),
		"group_formed": order.GetGroupFormed(), "group_failed": order.GetGroupFailed(),
		"resources_committed": order.GetResourcesCommitted(), "expires_at": timestamp(order.GetExpiresAt()),
		"created_at": timestamp(order.GetCreatedAt()), "updated_at": timestamp(order.GetUpdatedAt()),
		"quote": quote, "address": address, "items": quote["items"],
		"coupon_id": quote["coupon_id"], "points": quote["points"], "reservation_id": order.GetId(),
	}
	if order.GetNote() != "" {
		result["note"] = order.GetNote()
	}
	return result
}

func ListResult(response *mallv1.ListOrdersResponse) map[string]any {
	items := make([]any, 0, len(response.GetItems()))
	for _, order := range response.GetItems() {
		items = append(items, Result(order))
	}
	return map[string]any{"items": items}
}

func PaymentResult(response *mallv1.AcceptPaymentResponse) map[string]any {
	return map[string]any{"accepted": response.GetAccepted(), "order_id": response.GetOrderId(), "status": response.GetStatus(), "payment_id": response.GetPaymentId()}
}

func Actions() map[string]string {
	return map[string]string{
		mallv1.OrderService_ListOrders_FullMethodName:       "list",
		mallv1.OrderService_GetOrder_FullMethodName:         "get",
		mallv1.OrderService_CreateOrder_FullMethodName:      "create",
		mallv1.OrderService_MarkPending_FullMethodName:      "pending",
		mallv1.OrderService_MarkCreateFailed_FullMethodName: "create_failed",
		mallv1.OrderService_AcceptPayment_FullMethodName:    "accept_payment",
		mallv1.OrderService_MarkReady_FullMethodName:        "ready",
		mallv1.OrderService_WaitForGroup_FullMethodName:     "wait_group",
		mallv1.OrderService_BeginClose_FullMethodName:       "close_begin",
		mallv1.OrderService_CompleteClose_FullMethodName:    "close_complete",
		mallv1.OrderService_MarkGroupFormed_FullMethodName:  "group_formed",
		mallv1.OrderService_MarkGroupFailed_FullMethodName:  "group_failed",
		mallv1.OrderService_MarkShipped_FullMethodName:      "shipped",
		mallv1.OrderService_CompleteOrder_FullMethodName:    "complete",
		mallv1.OrderService_RecordRefund_FullMethodName:     "refunded",
	}
}
