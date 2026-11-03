package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IssueReward and Claim share the template lock. An earlier free claim cannot be
// mistaken for successful issuance of a paid points reward.
func (r *Repository) IssueReward(ctx context.Context, c platform.Request) (platform.Result, error) {
	var receipt domain.RewardReceipt
	var out platform.Result
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&receipt, "id = ?", c.ID).Error
		if er == nil {
			if receipt.UserID != c.UserID || receipt.TemplateID != platform.Text(c.Data, "template_id") {
				return status.Error(codes.AlreadyExists, "reward issuance differs")
			}
			out = platform.Result{"coupon_id": receipt.CouponID}
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		child := &Repository{DB: tx}
		out, er = child.Claim(ctx, c, func(_ context.Context, t domain.Template) error {
			if t.MinPoints != 0 || t.MinGrowth != 0 {
				return status.Error(codes.FailedPrecondition, "reward template must have no member threshold")
			}
			return nil
		})
		if er != nil {
			return er
		}
		if out["already_claimed"] == true {
			// Current read also handles another delivery of this same redemption.
			if er = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&receipt, "id = ?", c.ID).Error; er == nil &&
				receipt.UserID == c.UserID &&
				receipt.TemplateID == platform.Text(c.Data, "template_id") {
				out = platform.Result{"coupon_id": receipt.CouponID}
				return nil
			}
			if er != nil && !errors.Is(er, gorm.ErrRecordNotFound) {
				return er
			}
			return status.Error(codes.FailedPrecondition, "该优惠券每人限领一次，已领过的兑换将返还积分")
		}
		receipt = domain.RewardReceipt{
			ID:         c.ID,
			UserID:     c.UserID,
			TemplateID: platform.Text(c.Data, "template_id"),
			CouponID:   platform.Text(out, "id"),
		}
		if er = tx.Create(&receipt).Error; er != nil {
			return er
		}
		out = platform.Result{"coupon_id": receipt.CouponID}
		return nil
	})
	return out, e
}
