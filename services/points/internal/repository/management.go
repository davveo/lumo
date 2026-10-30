package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"strings"
)

func (r *Repository) Policy(ctx context.Context) (commercepolicy.Points, error) {
	v, version, e := adminconfig.Read(ctx, r.DB, "points-policy", commercepolicy.DefaultPoints())
	v.Version = version
	return v, e
}
func (r *Repository) PolicyUpsert(ctx context.Context, c platform.Request, p commercepolicy.Points) (platform.Result, error) {
	return adminconfig.Save(ctx, r.DB, "points-policy", p, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
}
func (r *Repository) AdminGet(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "points.repository.AdminGet")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var v domain.Account
	e := r.DB.WithContext(ctx).Where("user_id = ?", c.ID).Find(&v).Error
	if e != nil {
		return nil, e
	}
	v.UserID = c.ID
	return platform.Map(v), nil
}
func (r *Repository) Ledger(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "points.repository.Ledger")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	rows, total, f, e := adminquery.Page[domain.Ledger](ctx, r.DB.WithContext(ctx).Where("user_id = ?", c.ID), c.Data, adminquery.Spec{State: "kind", Search: []string{"id", "order_id", "reason", "actor_id"}})
	return adminquery.Result(rows, total, f), e
}
func (r *Repository) Adjust(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "points.repository.Adjust")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	amount := platform.Int(c.Data, "amount")
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	var input struct {
		Expected *int64 `json:"expected_available"`
	}
	if e := platform.Decode(c.Data, &input); e != nil {
		return nil, e
	}
	if c.ID == "" || len(c.ID) > 80 || c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 || amount == 0 || amount < -1000000 || amount > 1000000 || reason == "" || len([]rune(reason)) > 240 || input.Expected == nil {
		return nil, status.Error(codes.InvalidArgument, "member, nonzero adjustment up to 1000000, expected balance, reason and idempotency key required")
	}
	hash := sha256.Sum256([]byte(c.UserID + "\x00" + c.IdempotencyKey))
	id := fmt.Sprintf("manual:%x", hash[:16])
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, e := account(ctx, tx, c.ID)
		if e != nil {
			return e
		}
		var old domain.Ledger
		if e = tx.WithContext(ctx).Where("id = ?", id).Find(&old).Error; e != nil {
			return e
		}
		if old.ID != "" {
			if old.UserID != c.ID || old.Amount != amount || old.Reason != reason {
				return status.Error(codes.AlreadyExists, "adjustment key reused with different data")
			}
			return nil
		}
		if a.Available != *input.Expected {
			return status.Error(codes.Aborted, "points balance changed; reload before adjusting")
		}
		if amount < 0 && a.Available < -amount {
			return status.Error(codes.FailedPrecondition, "cannot deduct frozen points or create a negative available balance")
		}
		if a.Available > 9007199254740991-amount {
			return status.Error(codes.OutOfRange, "points balance exceeds supported range")
		}
		// Positive compensation pays existing debt first, just like earned credits.
		credited := amount
		debt := a.Debt
		if credited > 0 && debt > 0 {
			paid := credited
			if paid > debt {
				paid = debt
			}
			debt -= paid
			credited -= paid
		}
		if e = tx.WithContext(ctx).Model(&domain.Account{}).Where("user_id = ?", c.ID).Updates(map[string]any{"available": a.Available + credited, "debt": debt}).Error; e != nil {
			return e
		}
		return tx.WithContext(ctx).Create(&domain.Ledger{ID: id, UserID: c.ID, Amount: amount, Kind: constants.PointsLedgerKindManual, ActorID: c.UserID, Reason: reason}).Error
	})
	if e != nil {
		return nil, e
	}
	return r.AdminGet(ctx, c)
}
