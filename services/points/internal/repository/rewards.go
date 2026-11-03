package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

func rewardHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}
func (r *Repository) Offers(ctx context.Context, admin bool) (platform.Result, error) {
	rows := []domain.RedeemOffer{}
	q := r.DB.WithContext(ctx)
	now := time.Now().UTC()
	if !admin {
		q = q.Where("active = ? AND starts_at <= ? AND ends_at > ?", true, now, now)
	}
	e := q.Order("updated_at DESC,id").Limit(200).Find(&rows).Error
	return platform.Result{"items": rows, "limit": 200}, e
}
func (r *Repository) Offer(ctx context.Context, id string) (domain.RedeemOffer, error) {
	var row domain.RedeemOffer
	e := r.DB.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "兑换项目不存在")
	}
	return row, e
}

func (r *Repository) OfferUpsert(
	ctx context.Context,
	c platform.Request,
	input domain.RedeemOffer,
) (platform.Result, error) {
	var row domain.RedeemOffer
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if c.ID == "" {
			id, e := platform.NextBusinessID(ctx, tx, "reward-offer")
			if e != nil {
				return e
			}
			input.ID = id
			input.Version = 1
			input.Used = 0
			row = input
			return tx.Create(&row).Error
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", c.ID).Error; e != nil {
			return e
		}
		if row.Version != input.Version {
			return status.Error(codes.Aborted, "兑换项目已变更，请刷新")
		}
		if input.TotalQuota < row.Used {
			return status.Error(codes.FailedPrecondition, "总名额不能少于已兑换数量")
		}
		if row.Used > 0 && (row.Kind != input.Kind || row.ReferenceID != input.ReferenceID) {
			return status.Error(codes.FailedPrecondition, "已兑换项目不能更换奖品，请新建项目")
		}
		input.ID = row.ID
		input.Used = row.Used
		input.Version = row.Version + 1
		row = input
		return tx.Save(&row).Error
	})
	return platform.Map(row), e
}
func (r *Repository) Replay(ctx context.Context, c platform.Request) (domain.Redemption, bool, error) {
	var row domain.Redemption
	e := r.DB.WithContext(ctx).Where("request_key = ?", rewardHash(c.UserID+"\x00"+c.IdempotencyKey)).First(&row).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return row, false, nil
	}
	if e != nil {
		return row, false, e
	}
	if row.RequestHash != redemptionHash(c) {
		return row, false, status.Error(codes.AlreadyExists, "兑换请求内容不同")
	}
	return row, true, nil
}
func redemptionHash(c platform.Request) string {
	return rewardHash(c.ID + "\x00" + platform.Text(c.Data, "address_id"))
}
func (r *Repository) Redeem(ctx context.Context, c platform.Request, address platform.Result) (platform.Result, error) {
	var row domain.Redemption
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, e := account(ctx, tx, c.UserID)
		if e != nil {
			return e
		}
		child := &Repository{DB: tx}
		if old, ok, e := child.Replay(ctx, c); e != nil {
			return e
		} else if ok {
			row = old
			return nil
		}
		var offer domain.RedeemOffer
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&offer, "id = ?", c.ID).Error; e != nil {
			return e
		}
		now := time.Now().UTC()
		if offer.Version != platform.Int(c.Data, "version") {
			return status.Error(codes.Aborted, "积分或项目规则已更新，请刷新后确认")
		}
		if !offer.Active || now.Before(offer.StartsAt) || !now.Before(offer.EndsAt) {
			return status.Error(codes.FailedPrecondition, "兑换尚未开放或已结束")
		}
		if offer.Used >= offer.TotalQuota {
			return status.Error(codes.ResourceExhausted, "兑换名额已用完")
		}
		var count int64
		if e = tx.Model(&domain.Redemption{}).Where("user_id = ? AND offer_id = ? AND state <> ?", c.UserID, offer.ID, constants.RedemptionStateRefunded).Count(&count).Error; e != nil {
			return e
		}
		if count >= offer.MaxPerUser {
			return status.Error(codes.ResourceExhausted, "已达个人兑换上限")
		}
		if a.Debt > 0 || a.Available < offer.Cost {
			return status.Error(codes.ResourceExhausted, "可用积分不足或存在待回收积分")
		}
		id, e := platform.NextBusinessID(ctx, tx, "redemption")
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(address)
		row = domain.Redemption{
			ID:          id,
			RequestKey:  rewardHash(c.UserID + "\x00" + c.IdempotencyKey),
			RequestHash: redemptionHash(c),
			UserID:      c.UserID,
			OfferID:     offer.ID,
			Name:        offer.Name,
			Kind:        offer.Kind,
			ReferenceID: offer.ReferenceID,
			Cost:        offer.Cost,
			AddressJSON: string(raw),
			State:       constants.RedemptionStatePending,
			Version:     1,
			NextTry:     now,
		}
		if e = tx.Create(&row).Error; e != nil {
			return e
		}
		if e = tx.Model(&a).Update("available", gorm.Expr("available - ?", offer.Cost)).Error; e != nil {
			return e
		}
		if e = tx.Model(&offer).Update("used", gorm.Expr("used + 1")).Error; e != nil {
			return e
		}
		if e = tx.Create(&domain.Ledger{
			ID:      id + ":redeem",
			UserID:  c.UserID,
			OrderID: id,
			Amount:  -offer.Cost,
			Kind:    constants.PointsLedgerKindRedemption,
			Reason:  offer.Name,
		}).Error; e != nil {
			return e
		}
		return platform.Enqueue(
			ctx,
			tx,
			platform.Event{
				ID:          id + ":requested",
				Type:        constants.EventRedemptionRequested,
				AggregateID: id,
				UserID:      c.UserID,
			},
		)
	})
	return platform.Map(row), e
}
func (r *Repository) Redemption(ctx context.Context, id, user string) (domain.Redemption, error) {
	var row domain.Redemption
	e := r.DB.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "兑换单不存在")
	}
	if e == nil && user != "" && row.UserID != user {
		e = status.Error(codes.PermissionDenied, "兑换单归属不匹配")
	}
	return row, e
}
func (r *Repository) Redemptions(ctx context.Context, c platform.Request, admin bool) (platform.Result, error) {
	page, size := platform.Int(c.Data, "page"), platform.Int(c.Data, "page_size")
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 10
	}
	if page > 100000 {
		return nil, status.Error(codes.InvalidArgument, "page too large")
	}
	rows := []domain.Redemption{}
	q := r.DB.WithContext(ctx).Model(&domain.Redemption{})
	if !admin {
		q = q.Where("user_id = ?", c.UserID)
	}
	var count int64
	if e := q.Count(&count).Error; e != nil {
		return nil, e
	}
	e := q.Order("created_at DESC,id DESC").Limit(int(size)).Offset(int((page - 1) * size)).Find(&rows).Error
	return platform.Result{"items": rows, "total": count, "page": page, "page_size": size}, e
}
func (r *Repository) AcquireRedemption(ctx context.Context, id string) (domain.Redemption, bool, error) {
	var row domain.Redemption
	acquired := false
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; e != nil {
			return e
		}
		now := time.Now().UTC()
		if row.State != constants.RedemptionStatePending && row.State != constants.RedemptionStateProcessing {
			return nil
		}
		if row.LeaseUntil != nil && row.LeaseUntil.After(now) {
			return nil
		}
		until := now.Add(90 * time.Second)
		row.LeaseToken = platform.ID("reward-lease")
		row.LeaseUntil = &until
		row.Attempts++
		row.State = constants.RedemptionStateProcessing
		row.Version++
		acquired = true
		return tx.Save(&row).Error
	})
	return row, acquired, e
}

func (r *Repository) FinishRedemption(
	ctx context.Context,
	input domain.Redemption,
	state, coupon, fulfillment, problem string,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock accounts before redemptions, matching redemption creation/refunds.
		a, e := account(ctx, tx, input.UserID)
		if e != nil {
			return e
		}
		var row domain.Redemption
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", input.ID).Error; e != nil {
			return e
		}
		if row.LeaseToken != input.LeaseToken || row.State == constants.RedemptionStateRefunded {
			return nil
		}
		if row.State == constants.RedemptionStateShipped || row.State == constants.RedemptionStateDelivered {
			state = row.State
		}
		row.State = state
		row.LeaseUntil = nil
		row.LeaseToken = ""
		row.Version++
		row.LastError = string([]rune(problem)[:min(len([]rune(problem)), 120)])
		if coupon != "" {
			row.CouponID = coupon
		}
		if fulfillment != "" {
			row.FulfillmentID = fulfillment
		}
		row.NextTry = time.Now().UTC().Add(time.Minute)
		if state == constants.RedemptionStatePending && row.Attempts >= 10 {
			row.State = constants.RedemptionStateAttention
		}
		if state == constants.RedemptionStateRefunded {
			debtPaid := min(a.Debt, row.Cost)
			if e = tx.Model(&a).Updates(map[string]any{"debt": a.Debt - debtPaid, "available": a.Available + row.Cost - debtPaid}).Error; e != nil {
				return e
			}
			if e = tx.Model(&domain.RedeemOffer{}).Where("id = ? AND used > 0", row.OfferID).Update("used", gorm.Expr("used - 1")).Error; e != nil {
				return e
			}
			if e = tx.Create(&domain.Ledger{
				ID:      row.ID + ":reversal",
				UserID:  row.UserID,
				OrderID: row.ID,
				Amount:  row.Cost,
				Kind:    constants.PointsLedgerKindRedemptionReversal,
				Reason:  problem,
			}).Error; e != nil {
				return e
			}
		}
		if e = tx.Save(&row).Error; e != nil {
			return e
		}
		eventType := ""
		if state == constants.RedemptionStateIssued {
			eventType = constants.EventRedemptionIssued
		}
		if state == constants.RedemptionStateRefunded {
			eventType = constants.EventRedemptionRefunded
		}
		if eventType != "" {
			return platform.Enqueue(
				ctx,
				tx,
				platform.Event{
					ID:          row.ID + ":" + eventType,
					Type:        eventType,
					AggregateID: row.ID,
					UserID:      row.UserID,
					Data:        map[string]any{"name": row.Name, "cost": row.Cost, "kind": row.Kind},
				},
			)
		}
		return nil
	})
}
func (r *Repository) QueueRedemptions(ctx context.Context) error {
	now := time.Now().UTC()
	rows := []domain.Redemption{}
	if e := r.DB.WithContext(ctx).Where("state IN ? AND next_try <= ? AND (lease_until IS NULL OR lease_until <= ?)", []string{constants.RedemptionStatePending, constants.RedemptionStateProcessing}, now, now).Order("next_try,id").Limit(100).Find(&rows).Error; e != nil {
		return e
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			if e := platform.Enqueue(ctx, tx, platform.Event{
				ID:          row.ID + ":recover:" + now.Format("200601021504"),
				Type:        constants.EventRedemptionRecoveryRequested,
				AggregateID: row.ID,
				UserID:      row.UserID,
			}); e != nil {
				return e
			}
		}
		return nil
	})
}
func (r *Repository) RetryRedemption(ctx context.Context, c platform.Request) (platform.Result, error) {
	if strings.TrimSpace(platform.Text(c.Data, "reason")) == "" {
		return nil, status.Error(codes.InvalidArgument, "重试原因必填")
	}
	row, e := r.Redemption(ctx, c.ID, "")
	if e != nil {
		return nil, e
	}
	res := r.DB.WithContext(ctx).
		Model(&domain.Redemption{}).
		Where("id = ? AND state = ? AND version = ?", c.ID, constants.RedemptionStateAttention, platform.Int(c.Data, "version")).
		Updates(map[string]any{
			"state":      constants.RedemptionStatePending,
			"attempts":   0,
			"next_try":   time.Now().UTC(),
			"last_error": "管理员 " + c.UserID + " 重试：" + platform.Text(c.Data, "reason"),
			"version":    gorm.Expr("version + 1"),
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, status.Error(codes.Aborted, "仅可重试待跟进兑换单，请刷新")
	}
	return platform.Result{"id": row.ID, "queued": true}, nil
}
func (r *Repository) RewardShipment(ctx context.Context, event platform.Event) error {
	target := constants.RedemptionStateShipped
	if event.Type == constants.EventRewardDelivered {
		target = constants.RedemptionStateDelivered
	}
	return r.DB.WithContext(ctx).
		Model(&domain.Redemption{}).
		Where("id = ? AND user_id = ? AND state IN ?", platform.Text(event.Data, "order_id"), event.UserID, []string{
			constants.RedemptionStatePending,
			constants.RedemptionStateProcessing,
			constants.RedemptionStateAttention,
			constants.RedemptionStateIssued,
			constants.RedemptionStateShipped,
		}).
		Updates(map[string]any{
			"state":          target,
			"tracking_id":    platform.Text(event.Data, "tracking_id"),
			"fulfillment_id": event.AggregateID,
			"version":        gorm.Expr("version + 1"),
		}).
		Error
}
