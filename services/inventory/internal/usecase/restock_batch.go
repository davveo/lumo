package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"sort"
)

func (s *Service) RestockBatch(ctx context.Context, c platform.Request) (platform.Result, error) {
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal inventory command required")
	}
	var in struct {
		Items []struct {
			ID        string `json:"id"`
			SKU       string `json:"sku_id"`
			Quantity  int64  `json:"quantity"`
			Condition string `json:"condition"`
		} `json:"items"`
	}
	if err := platform.Decode(c.Data, &in); err != nil {
		return nil, err
	}
	if len(in.Items) == 0 || len(in.Items) > 100 {
		return nil, status.Error(codes.InvalidArgument, "return batch requires 1..100 receipts")
	}
	sort.Slice(in.Items, func(i, j int) bool {
		if in.Items[i].SKU != in.Items[j].SKU {
			return in.Items[i].SKU < in.Items[j].SKU
		}
		return in.Items[i].ID < in.Items[j].ID
	})
	seen := map[string]bool{}
	for _, item := range in.Items {
		if item.ID == "" || len(item.ID) > 120 || seen[item.ID] || len(item.SKU) > 80 {
			return nil, status.Error(codes.InvalidArgument, "unique bounded return receipt IDs required")
		}
		seen[item.ID] = true
	}
	out := []platform.Result{}
	err := s.Repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := &repository.Repository{DB: tx}
		for _, item := range in.Items {
			v, e := repo.Restock(
				ctx,
				platform.Request{
					ID:     item.ID,
					UserID: c.UserID,
					Role:   c.Role,
					Data:   map[string]any{"sku_id": item.SKU, "quantity": item.Quantity, "condition": item.Condition},
				},
			)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return nil
	})
	return platform.Result{"items": out}, err
}
