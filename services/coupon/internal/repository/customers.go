package repository

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/domain"
	"time"
)

func (r *Repository) CustomerAssets(ctx context.Context, id string) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "coupon.repository.CustomerAssets")
	defer sp.End()
	var counts struct {
		Total     int64 `json:"total"`
		Available int64 `json:"available"`
		Frozen    int64 `json:"frozen"`
		Used      int64 `json:"used"`
		Expired   int64 `json:"expired"`
	}
	e := r.DB.WithContext(ctx).
		Model(&domain.Coupon{}).
		Where("user_id = ?", id).
		Select("COUNT(*) AS total, COUNT(CASE WHEN state = ? AND expires_at > ? THEN 1 END) AS available, COUNT(CASE WHEN state = ? THEN 1 END) AS frozen, COUNT(CASE WHEN state = ? THEN 1 END) AS used, COUNT(CASE WHEN expires_at <= ? AND state = ? THEN 1 END) AS expired", constants.CouponStatusAvailable, time.Now(), constants.CouponStatusHeld, constants.CouponStatusUsed, time.Now(), constants.CouponStatusAvailable).
		Scan(&counts).
		Error
	return platform.Map(counts), e
}
