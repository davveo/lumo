package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/fulfillment/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) CreateGift(ctx context.Context, c platform.Request) (platform.Result, error) {
	if c.Role != constants.RoleSystem || c.UserID == "" {
		return nil, status.Error(codes.PermissionDenied, "internal gift fulfillment required")
	}
	var input struct {
		Address map[string]any `json:"address"`
		Lines   []struct {
			SKU      string `json:"sku_id"`
			Quantity int64  `json:"quantity"`
		} `json:"lines"`
	}
	if e := platform.Decode(c.Data, &input); e != nil {
		return nil, e
	}
	if !platform.IsBusinessID(c.ID, "redemption") || len(input.Lines) != 1 || input.Lines[0].SKU == "" ||
		input.Lines[0].Quantity != 1 ||
		platform.Text(input.Address, "user_id") != c.UserID ||
		platform.Text(input.Address, "address") == "" && platform.Text(input.Address, "detail") == "" {
		return nil, status.Error(codes.InvalidArgument, "valid gift, owner and address snapshot required")
	}
	lines, _ := json.Marshal(input.Lines)
	address, _ := json.Marshal(input.Address)
	row := &domain.Record{
		ID:            c.ID,
		OrderID:       c.ID,
		ReservationID: c.ID,
		UserID:        c.UserID,
		Kind:          constants.FulfillmentKindGift,
		Status:        constants.FulfillmentStatusReady,
		LinesJSON:     string(lines),
		AddressJSON:   string(address),
	}
	if e := s.Repo.Create(ctx, row); e != nil {
		return nil, e
	}
	return platform.Map(row), nil
}
