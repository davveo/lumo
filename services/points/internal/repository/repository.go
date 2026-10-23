package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/big"
	"time"
)

type Repository struct{ DB *gorm.DB }

func (r *Repository) Migrate(ctx context.Context) error {
	return r.DB.WithContext(ctx).AutoMigrate(&domain.Account{}, &domain.Hold{}, &domain.Ledger{}, &domain.Reward{})
}
func (r *Repository) Seed(ctx context.Context) error {
	return r.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&domain.Account{UserID: "demo", Available: 5000}).Error
}
func account(ctx context.Context, tx *gorm.DB, user string) (domain.Account, error) {
	v := domain.Account{UserID: user}
	e := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&v).Error
	if e != nil {
		return v, e
	}
	e = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&v, "user_id = ?", user).Error
	return v, e
}
func (r *Repository) Get(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.points.Get")
	defer s.End()
	var a domain.Account
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { var er error; a, er = account(ctx, tx, c.UserID); return er })
	if e != nil {
		return nil, e
	}
	var ls []domain.Ledger
	e = r.DB.WithContext(ctx).Where("user_id = ?", c.UserID).Order("created_at DESC").Limit(100).Find(&ls).Error
	out := platform.Map(a)
	out["ledger"] = ls
	return out, e
}
func (r *Repository) Reserve(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.points.Reserve")
	defer s.End()
	n := platform.Int(c.Data, "amount")
	if c.ID == "" || n < 0 || n > 100000000 {
		return nil, status.Error(codes.InvalidArgument, "order and valid amount required")
	}
	var out domain.Hold
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, lockErr := account(ctx, tx, c.UserID); lockErr != nil {
			return lockErr
		}
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", c.ID).Error
		if er == nil {
			if out.UserID != c.UserID {
				return status.Error(codes.PermissionDenied, "hold owner mismatch")
			}
			if out.State == "CANCELLED" {
				return status.Error(codes.FailedPrecondition, "hold cancelled")
			}
			if out.Amount != n {
				return status.Error(codes.AlreadyExists, "hold amount differs")
			}
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		out = domain.Hold{ID: c.ID, UserID: c.UserID, Amount: n, State: "RESERVED"}
		if er = tx.WithContext(ctx).Create(&out).Error; er != nil {
			return er
		}
		u := tx.WithContext(ctx).Model(&domain.Account{}).Where("user_id = ? AND available >= ? AND debt = 0", c.UserID, n).Updates(map[string]any{"available": gorm.Expr("available - ?", n), "frozen": gorm.Expr("frozen + ?", n)})
		if u.Error != nil {
			return u.Error
		}
		if u.RowsAffected != 1 {
			return status.Error(codes.ResourceExhausted, "insufficient points or outstanding debt")
		}
		return tx.WithContext(ctx).Create(&domain.Ledger{ID: c.ID + ":reserve", UserID: c.UserID, OrderID: c.ID, Amount: -n, Kind: "FREEZE"}).Error
	})
	return platform.Map(out), e
}
func (r *Repository) Transition(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.points."+c.Action)
	defer s.End()
	if c.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "order required")
	}
	var out domain.Hold
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, lockErr := account(ctx, tx, c.UserID); lockErr != nil {
			return lockErr
		}
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", c.ID).Error
		if errors.Is(er, gorm.ErrRecordNotFound) && c.Action == "cancel" {
			out = domain.Hold{ID: c.ID, UserID: c.UserID, State: "CANCELLED"}
			return tx.WithContext(ctx).Create(&out).Error
		}
		if er != nil {
			if !errors.Is(er, gorm.ErrRecordNotFound) {
				return er
			}
			return status.Error(codes.NotFound, "points hold not found")
		}
		if out.UserID != c.UserID {
			return status.Error(codes.PermissionDenied, "hold owner mismatch")
		}
		target := "CONFIRMED"
		if c.Action == "cancel" {
			target = "CANCELLED"
		}
		if out.State == target {
			return nil
		}
		if out.State != "RESERVED" {
			return status.Error(codes.FailedPrecondition, "invalid points transition")
		}
		updates := map[string]any{"frozen": gorm.Expr("frozen - ?", out.Amount)}
		if target == "CANCELLED" {
			updates["available"] = gorm.Expr("available + ?", out.Amount)
		}
		u := tx.WithContext(ctx).Model(&domain.Account{}).Where("user_id = ? AND frozen >= ?", out.UserID, out.Amount).Updates(updates)
		if u.Error != nil {
			return u.Error
		}
		if u.RowsAffected != 1 {
			return status.Error(codes.Internal, "points account invariant violated")
		}
		if er = tx.WithContext(ctx).Model(&out).Update("state", target).Error; er != nil {
			return er
		}
		out.State = target
		return tx.WithContext(ctx).Create(&domain.Ledger{ID: c.ID + ":" + target, UserID: out.UserID, OrderID: c.ID, Amount: out.Amount, Kind: target}).Error
	})
	return platform.Map(out), e
}
func (r *Repository) Credit(ctx context.Context, c platform.Command, checkin bool) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.points.Credit")
	defer s.End()
	n := platform.Int(c.Data, "amount")
	id := "reward:" + c.ID
	kind := "REWARD"
	order := c.ID
	if checkin {
		n = 20
		id = "checkin:" + c.UserID + ":" + time.Now().UTC().Format("2006-01-02")
		kind = "CHECKIN"
		order = ""
	}
	if n < 0 || n > 100000000 || (!checkin && c.ID == "") {
		return nil, status.Error(codes.InvalidArgument, "valid reward amount/order required")
	}
	result := platform.Result{"credited": n, "id": id}
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, er := account(ctx, tx, c.UserID)
		if er != nil {
			return er
		}
		var prior domain.Ledger
		er = tx.WithContext(ctx).First(&prior, "id = ?", id).Error
		if er == nil {
			if prior.UserID != c.UserID || prior.Amount != n {
				return status.Error(codes.AlreadyExists, "credit differs")
			}
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		if er = tx.WithContext(ctx).Create(&domain.Ledger{ID: id, UserID: c.UserID, OrderID: order, Amount: n, Kind: kind}).Error; er != nil {
			return er
		}
		if !checkin {
			if er = tx.WithContext(ctx).Create(&domain.Reward{OrderID: order, UserID: c.UserID, Amount: n}).Error; er != nil {
				return er
			}
		}
		debtPaid := n
		if debtPaid > a.Debt {
			debtPaid = a.Debt
		}
		result["debt_repaid"] = debtPaid
		return tx.WithContext(ctx).Model(&a).Updates(map[string]any{"available": gorm.Expr("available + ?", n-debtPaid), "debt": gorm.Expr("debt - ?", debtPaid)}).Error
	})
	return result, e
}
func (r *Repository) Refund(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.points.Refund")
	defer s.End()
	order := platform.Text(c.Data, "order_id")
	payable := platform.Int(c.Data, "payable_cents")
	total := platform.Int(c.Data, "refunded_total_cents")
	if c.ID == "" || order == "" || payable <= 0 || total <= 0 || total > payable {
		return nil, status.Error(codes.InvalidArgument, "refund, order, payable and cumulative refund required")
	}
	out := platform.Result{"returned": int64(0), "reward_reversed": int64(0)}
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, er := account(ctx, tx, c.UserID)
		if er != nil {
			return er
		}
		var prior domain.Ledger
		er = tx.WithContext(ctx).First(&prior, "id = ?", "refund:"+c.ID).Error
		if er == nil {
			if prior.UserID != c.UserID || prior.OrderID != order {
				return status.Error(codes.AlreadyExists, "refund differs")
			}
			out["returned"] = prior.Amount
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		returned := int64(0)
		var hold domain.Hold
		er = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&hold, "id = ?", order).Error
		if er == nil {
			if hold.UserID != c.UserID {
				return status.Error(codes.PermissionDenied, "hold owner mismatch")
			}
			if hold.State != "CONFIRMED" && hold.State != "REFUNDED" {
				return status.Error(codes.FailedPrecondition, "points not consumed")
			}
			target := portion(hold.Amount, total, payable)
			if total == payable {
				target = hold.Amount
			}
			returned = target - hold.Returned
			if returned < 0 {
				returned = 0
			}
			if er = tx.WithContext(ctx).Model(&hold).Updates(map[string]any{"returned": hold.Returned + returned, "state": func() string {
				if total == payable {
					return "REFUNDED"
				}
				return "CONFIRMED"
			}()}).Error; er != nil {
				return er
			}
		} else if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		a.Available += returned
		reversed := int64(0)
		var reward domain.Reward
		er = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&reward, "order_id = ?", order).Error
		if er == nil {
			if reward.UserID != c.UserID {
				return status.Error(codes.PermissionDenied, "reward owner mismatch")
			}
			target := portion(reward.Amount, total, payable)
			if total == payable {
				target = reward.Amount
			}
			reversed = target - reward.Reversed
			if reversed < 0 {
				reversed = 0
			}
			cash := reversed
			if cash > a.Available {
				cash = a.Available
			}
			a.Available -= cash
			a.Debt += reversed - cash
			if er = tx.WithContext(ctx).Model(&reward).Update("reversed", reward.Reversed+reversed).Error; er != nil {
				return er
			}
		} else if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		if er = tx.WithContext(ctx).Model(&a).Updates(map[string]any{"available": a.Available, "debt": a.Debt}).Error; er != nil {
			return er
		}
		if er = tx.WithContext(ctx).Create(&domain.Ledger{ID: "refund:" + c.ID, UserID: c.UserID, OrderID: order, Amount: returned, Kind: "REFUND"}).Error; er != nil {
			return er
		}
		if reversed > 0 {
			if er = tx.WithContext(ctx).Create(&domain.Ledger{ID: "reversal:" + c.ID, UserID: c.UserID, OrderID: order, Amount: -reversed, Kind: "REWARD_REVERSAL"}).Error; er != nil {
				return er
			}
		}
		out["returned"] = returned
		out["reward_reversed"] = reversed
		return nil
	})
	return out, e
}

func portion(amount, total, payable int64) int64 {
	var n big.Int
	n.Mul(big.NewInt(amount), big.NewInt(total))
	n.Quo(&n, big.NewInt(payable))
	return n.Int64()
}
