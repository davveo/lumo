package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"unicode/utf8"
)

func (r *Repository) Adjust(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "inventory.repository.Adjust")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var input struct {
		OnHand   int64  `json:"on_hand"`
		Expected *int64 `json:"expected_on_hand"`
		Reason   string `json:"reason"`
	}
	if err := platform.Decode(c.Data, &input); err != nil {
		return nil, err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if c.ID == "" || len(c.ID) > 80 || input.OnHand < 0 || input.OnHand > 9007199254740991 ||
		utf8.RuneCountInString(input.Reason) > 240 {
		return nil, status.Error(codes.InvalidArgument, "invalid stock quantity, SKU or adjustment reason")
	}
	if input.Expected != nil && input.Reason == "" {
		return nil, status.Error(codes.InvalidArgument, "stock adjustment reason required")
	}
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stock domain.Stock
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&stock, "sku_id = ?", c.ID).Error
		missing := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !missing {
			return err
		}
		if input.Expected != nil && stock.OnHand != *input.Expected {
			return status.Error(codes.Aborted, "stock changed; refresh before adjusting")
		}
		if input.OnHand < stock.Reserved+stock.Allocated {
			return status.Error(codes.FailedPrecondition, "cannot set stock below committed resources")
		}
		before := stock.OnHand
		if missing {
			err = tx.Create(&domain.Stock{SKUID: c.ID, OnHand: input.OnHand}).Error
		} else {
			err = tx.Model(&stock).Update("on_hand", input.OnHand).Error
		}
		if err != nil {
			return err
		}
		adjustmentID, e := platform.NextBusinessID(ctx, tx, "stock-adjustment")
		if e != nil {
			return e
		}
		return tx.WithContext(ctx).Create(
			&domain.Adjustment{
				ID:      adjustmentID,
				SKUID:   c.ID,
				ActorID: c.UserID,
				Before:  before,
				After:   input.OnHand,
				Reason:  input.Reason,
			},
		).Error
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, c.ID)
}
func (r *Repository) Adjustments(ctx context.Context, c platform.Request) (platform.Result, error) {
	if c.ID == "" || len(c.ID) > 80 {
		return nil, status.Error(codes.InvalidArgument, "SKU required")
	}
	if platform.Int(c.Data, "page") > 0 {
		rows, total, f, e := adminquery.Page[domain.Adjustment](ctx, r.DB.WithContext(ctx).Where("sku_id = ?", c.ID), c.Data, adminquery.Spec{State: "actor_id", Search: []string{"reason", "actor_id"}})
		return adminquery.Result(rows, total, f), e
	}
	items := []domain.Adjustment{}
	err := r.DB.WithContext(ctx).Where("sku_id = ?", c.ID).Order("created_at DESC,id DESC").Limit(20).Find(&items).Error
	return platform.Result{"items": items, "limit": 20}, err
}
