package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/fulfillment/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

func (s *Service) InspectReturns(ctx context.Context, c platform.Request) (platform.Result, error) {
	if !internal(c) {
		return nil, status.Error(codes.PermissionDenied, "warehouse inspection permission required")
	}
	var in struct {
		Items []struct {
			ID        string `json:"id"`
			OrderID   string `json:"order_id"`
			SKU       string `json:"sku_id"`
			Quantity  int64  `json:"quantity"`
			Condition string `json:"condition"`
		} `json:"items"`
	}
	if err := platform.Decode(c.Data, &in); err != nil {
		return nil, err
	}
	if len(in.Items) == 0 || len(in.Items) > 100 || c.UserID == "" {
		return nil, status.Error(codes.InvalidArgument, "inspection batch requires owner and 1..100 receipts")
	}
	receipts := []*domain.Record{}
	restocks := []map[string]any{}
	seen := map[string]bool{}
	for _, item := range in.Items {
		item.Condition = strings.ToLower(item.Condition)
		if item.ID == "" || len(item.ID) > 93 || seen[item.ID] || item.SKU == "" || len(item.SKU) > 80 ||
			len(item.OrderID) > 100 ||
			item.Quantity < 1 ||
			item.Quantity > 100000 ||
			(item.Condition != constants.StockConditionSellable && item.Condition != constants.StockConditionDamaged && item.Condition != constants.StockConditionUnsellable) {
			return nil, status.Error(
				codes.InvalidArgument,
				"valid unique receipt, SKU, quantity and condition required",
			)
		}
		seen[item.ID] = true
		raw, _ := json.Marshal(
			map[string]any{"sku_id": item.SKU, "quantity": item.Quantity, "condition": item.Condition},
		)
		receipt := &domain.Record{
			ID:            "return:" + item.ID,
			OrderID:       item.OrderID,
			UserID:        c.UserID,
			ReservationID: item.ID,
			Kind:          constants.FulfillmentKindReturn,
			Status:        constants.FulfillmentStatusInspecting,
			LinesJSON:     string(raw),
		}
		receipts = append(receipts, receipt)
	}
	if err := s.Repo.EnsureReturnReceipts(ctx, receipts); err != nil {
		return nil, err
	}
	for _, receipt := range receipts {
		if receipt.Status != constants.FulfillmentStatusInspected {
			var payload map[string]any
			_ = json.Unmarshal([]byte(receipt.LinesJSON), &payload)
			payload["id"] = receipt.ReservationID
			restocks = append(restocks, payload)
		}
	}

	if len(restocks) > 0 {
		if _, err := platform.NewClients(s.RPC).Inventory.RestockBatch(ctx, platform.Request{UserID: c.UserID, Role: constants.RoleSystem, Data: map[string]any{"items": restocks}}); err != nil {
			return nil, err
		}
	}
	out := []platform.Result{}
	for _, receipt := range receipts {
		if receipt.Status != constants.FulfillmentStatusInspected {
			var data map[string]any
			_ = json.Unmarshal([]byte(receipt.LinesJSON), &data)
			data["inspection_id"] = receipt.ReservationID
			event := &platform.Event{
				ID:          receipt.ID + ":inspected",
				Type:        constants.EventReturnInspectionCompleted,
				AggregateID: receipt.ID,
				UserID:      receipt.UserID,
				Data:        data,
			}
			updated, err := s.Repo.Transition(
				ctx,
				receipt.ID,
				[]string{constants.FulfillmentStatusInspecting},
				constants.FulfillmentStatusInspected,
				nil,
				event,
			)
			if err != nil {
				return nil, err
			}
			receipt = updated
		}
		out = append(out, platform.Map(receipt))
	}
	return platform.Result{"items": out}, nil
}
