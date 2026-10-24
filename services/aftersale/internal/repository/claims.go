package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Claim serializes all aftersale applications for one order in the owning schema.
// Pending and completed applications consume quantity; rejected/cancelled ones do not.
func (r *Repository) Claim(ctx context.Context, row *domain.Record, purchased map[string]int64, payable int64) error {
	ctx, span := platform.Start(ctx, "repository.aftersale.ClaimQuantity")
	defer span.End()
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		guard := domain.OrderGuard{OrderID: row.OrderID}
		if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&guard).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(
			&guard,
			"order_id = ?",
			row.OrderID,
		).Error; err != nil {
			return err
		}
		var old domain.Record
		err := tx.WithContext(ctx).First(&old, "id = ?", row.ID).Error
		if err == nil {
			if old.UserID != row.UserID || old.OrderID != row.OrderID {
				return status.Error(codes.PermissionDenied, "aftersale identity conflict")
			}
			if old.Type != row.Type || old.LinesJSON != row.LinesJSON || old.ReplacementSKU != row.ReplacementSKU {
				return status.Error(codes.AlreadyExists, "idempotency key reused with a different aftersale request")
			}
			*row = old
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		existing := []domain.Record{}
		if err = tx.WithContext(ctx).Where(
			"order_id = ? AND status NOT IN ?",
			row.OrderID,
			[]string{constants.AftersaleStatusRejected, constants.AftersaleStatusCancelled},
		).Find(
			&existing,
		).Error; err != nil {
			return err
		}
		used := map[string]int64{}
		var claimedCash int64
		hasExchange := false
		for _, a := range existing {
			claimedCash += a.AmountCents
			if a.Type == constants.AftersaleTypeExchange {
				hasExchange = true
			}
			var lines []domain.Line
			if err = json.Unmarshal([]byte(a.LinesJSON), &lines); err != nil {
				return err
			}
			for _, l := range lines {
				used[l.SKUID] += l.Quantity
			}
		}
		var requested []domain.Line
		if err = json.Unmarshal([]byte(row.LinesJSON), &requested); err != nil {
			return err
		}
		if len(requested) == 0 {
			return status.Error(codes.InvalidArgument, "aftersale lines required")
		}
		for _, l := range requested {
			if l.Quantity <= 0 || purchased[l.SKUID] <= 0 || used[l.SKUID]+l.Quantity > purchased[l.SKUID] {
				return status.Error(codes.FailedPrecondition, "aftersale quantity exceeds purchased unclaimed quantity")
			}
			used[l.SKUID] += l.Quantity
		}
		// The last cash-return application absorbs integer allocation rounding.
		all := true
		for sku, qty := range purchased {
			if used[sku] != qty {
				all = false
			}
		}
		if all && row.Type != constants.AftersaleTypeExchange && !hasExchange {
			row.AmountCents = payable - claimedCash
		}
		if row.Type != constants.AftersaleTypeExchange && row.AmountCents <= 0 {
			return status.Error(codes.FailedPrecondition, "cash refund quota is exhausted")
		}
		if err = tx.WithContext(ctx).Create(row).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).
			Model(&domain.OrderGuard{}).
			Where("order_id = ?", guard.OrderID).
			UpdateColumn("version", gorm.Expr("version + 1")).
			Error
	})
}
func (r *Repository) ByReplacement(ctx context.Context, id string) (*domain.Record, error) {
	ctx, span := platform.Start(ctx, "repository.aftersale.ByReplacement")
	defer span.End()
	var row domain.Record
	err := r.DB.WithContext(ctx).First(&row, "replacement_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, status.Error(codes.NotFound, "exchange application not found")
	}
	return &row, err
}
