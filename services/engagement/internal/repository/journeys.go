package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *Repository) JourneyRules(ctx context.Context, c platform.Request) (platform.Result, error) {
	rows, total, f, e := adminquery.Page[domain.JourneyRule](
		ctx,
		r.db.WithContext(ctx),
		c.Data,
		adminquery.Spec{Search: []string{"id", "name", "sku_id"}},
	)
	return adminquery.Result(rows, total, f), e
}
func (r *Repository) JourneyRule(ctx context.Context, id string) (domain.JourneyRule, error) {
	var row domain.JourneyRule
	e := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "触达规则不存在")
	}
	return row, e
}
func (r *Repository) ActiveJourneyRules(ctx context.Context, kind string) ([]domain.JourneyRule, error) {
	rows := []domain.JourneyRule{}
	e := r.db.WithContext(ctx).Where("kind = ? AND enabled = ?", kind, true).Order("id").Limit(100).Find(&rows).Error
	return rows, e
}
func (r *Repository) SaveJourneyRule(ctx context.Context, row domain.JourneyRule) (domain.JourneyRule, error) {
	ctx, sp := platform.Start(ctx, "engagement.repository.SaveJourneyRule")
	defer sp.End()
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := domain.JourneyRule{ID: row.ID}
		if e := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; e != nil {
			return e
		}
		var old domain.JourneyRule
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", row.ID).Error; e != nil {
			return e
		}
		if row.Version != old.Version {
			return status.Error(codes.Aborted, "规则已更新，请刷新")
		}
		var n int64
		if e := tx.WithContext(ctx).Model(&domain.JourneyRule{}).Where("kind = ? AND id <> ? AND enabled = ?", row.Kind, row.ID, true).Count(&n).Error; e != nil {
			return e
		}
		if row.Enabled && n >= 100 {
			return status.Error(codes.ResourceExhausted, "每种类型最多100个启用规则")
		}
		row.Version++
		row.CreatedAt = old.CreatedAt
		return tx.WithContext(ctx).Save(&row).Error
	})
	return row, e
}
func journeyOwner(ctx context.Context, tx *gorm.DB, user string) (domain.JourneyOwner, error) {
	seed := domain.JourneyOwner{UserID: user, LastPaidAt: time.Unix(0, 0).UTC(), LastQueuedAt: time.Unix(0, 0).UTC()}
	if e := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; e != nil {
		return seed, e
	}
	e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&seed, "user_id = ?", user).Error
	return seed, e
}
func (r *Repository) SeedJourneys(ctx context.Context, seeds []domain.Journey, receipt string) error {
	ctx, sp := platform.Start(ctx, "engagement.repository.SeedJourneys")
	defer sp.End()
	if len(seeds) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		owner, e := journeyOwner(ctx, tx, seeds[0].UserID)
		if e != nil {
			return e
		}
		if receipt != "" {
			res := tx.WithContext(ctx).
				Clauses(clause.OnConflict{DoNothing: true}).
				Create(&domain.JourneyReceipt{ID: receipt})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil
			}
		}
		for _, seed := range seeds {
			if seed.Kind == constants.JourneyKindCart && !seed.SourceAt.After(owner.LastPaidAt) {
				continue
			}
			var old domain.Journey
			e := tx.WithContext(ctx).
				Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&old, "slot_key = ?", seed.SlotKey).
				Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if e == nil {
				if !seed.SourceAt.After(old.SourceAt) {
					continue
				}
				seed.Generation = old.Generation + 1
				if old.Status == constants.JourneyStatusWatching {
					seed.ID = old.ID
					seed.CreatedAt = old.CreatedAt
				} else {
					if e := tx.WithContext(ctx).Model(&old).Update("slot_key", old.ID).Error; e != nil {
						return e
					}
				}
			} else {
				seed.Generation = 1
			}
			if seed.ID == "" {
				seed.ID = platform.ID("journey")
			}
			seed.Status = constants.JourneyStatusWatching
			seed.LeaseUntil = time.Now().UTC()
			if e = tx.WithContext(ctx).Save(&seed).Error; e != nil {
				return e
			}
		}
		return nil
	})
}
func (r *Repository) Journey(ctx context.Context, id string) (domain.Journey, error) {
	var j domain.Journey
	e := r.db.WithContext(ctx).First(&j, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "触达记录不存在")
	}
	return j, e
}
func (r *Repository) Journeys(ctx context.Context, c platform.Request) (platform.Result, error) {
	rows, total, f, e := adminquery.Page[domain.Journey](
		ctx,
		r.db.WithContext(ctx),
		c.Data,
		adminquery.Spec{
			State:  "status",
			Owner:  "user_id",
			Search: []string{"id", "user_id", "rule_id", "source_id", "sku_id"},
		},
	)
	if e != nil {
		return nil, e
	}
	var counts struct {
		Queued    int64 `json:"queued"`
		Delivered int64 `json:"delivered"`
		Clicked   int64 `json:"clicked"`
		Recovered int64 `json:"recovered"`
	}
	e = r.db.WithContext(ctx).
		Model(&domain.Journey{}).
		Select("COALESCE(SUM(CASE WHEN queued_at IS NOT NULL THEN 1 ELSE 0 END),0) AS queued, COALESCE(SUM(CASE WHEN delivered_at IS NOT NULL THEN 1 ELSE 0 END),0) AS delivered, COALESCE(SUM(CASE WHEN clicked_at IS NOT NULL THEN 1 ELSE 0 END),0) AS clicked, COALESCE(SUM(CASE WHEN recovered_at IS NOT NULL THEN 1 ELSE 0 END),0) AS recovered").
		Scan(&counts).
		Error
	out := adminquery.Result(rows, total, f)
	out["counts"] = counts
	out["scope"] = "全量触达记录；恢复付款指点击后7天内匹配原订单或相同商品的付款，不代表触达带来的因果提升。"
	return out, e
}
func (r *Repository) ClaimJourney(ctx context.Context) (*domain.Journey, error) {
	now := time.Now().UTC()
	var j domain.Journey
	e := r.db.WithContext(ctx).
		Where("status = ? AND due_at <= ? AND (claim = '' OR lease_until <= ?)", constants.JourneyStatusWatching, now, now).
		Order("due_at,id").
		First(&j).
		Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	claim := platform.ID("lease")
	res := r.db.WithContext(ctx).
		Model(&j).
		Where("status = ? AND (claim = '' OR lease_until <= ?)", constants.JourneyStatusWatching, now).
		Updates(map[string]any{"claim": claim, "lease_until": now.Add(2 * time.Minute)})
	if res.Error != nil || res.RowsAffected != 1 {
		return nil, res.Error
	}
	j.Claim = claim
	return &j, nil
}

func (r *Repository) QueueJourney(
	ctx context.Context,
	j domain.Journey,
	rule domain.JourneyRule,
	skus []string,
	reason string,
	retry bool,
) error {
	ctx, sp := platform.Start(ctx, "engagement.repository.QueueJourney")
	defer sp.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		owner, e := journeyOwner(ctx, tx, j.UserID)
		if e != nil {
			return e
		}
		var held domain.Journey
		if e = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, "id = ?", j.ID).Error; e != nil {
			return e
		}
		if held.Status != constants.JourneyStatusWatching || held.Claim != j.Claim {
			return nil
		}
		held.Claim = ""
		held.LeaseUntil = time.Now().UTC()
		if held.Generation != j.Generation {
			return tx.WithContext(ctx).Save(&held).Error
		}
		now := time.Now().UTC()
		if reason != "" {
			held.Reason = reason
			if retry {
				held.DueAt = now.Add(5 * time.Minute)
			} else {
				held.Status = constants.JourneyStatusSuppressed
			}
			return tx.WithContext(ctx).Save(&held).Error
		}
		if held.Kind == constants.JourneyKindCart && !held.SourceAt.After(owner.LastPaidAt) {
			held.Status = constants.JourneyStatusExited
			held.Reason = "已发生付款，退出当前召回"
			return tx.WithContext(ctx).Save(&held).Error
		}
		next := owner.LastQueuedAt.Add(time.Duration(rule.GapHours) * time.Hour)
		if now.Before(next) {
			held.DueAt = next
			held.Reason = "频控等待：同一顾客跨规则共用发送间隔"
			return tx.WithContext(ctx).Save(&held).Error
		}
		raw, _ := json.Marshal(skus)
		held.SKUsJSON = string(raw)
		held.Status = constants.JourneyStatusQueued
		held.QueuedAt = &now
		held.Reason = "等待消息中心投递"
		owner.LastQueuedAt = now
		if e = tx.WithContext(ctx).Save(&owner).Error; e != nil {
			return e
		}
		if e = tx.WithContext(ctx).Save(&held).Error; e != nil {
			return e
		}
		return platform.Enqueue(
			ctx,
			tx,
			platform.Event{
				ID:          platform.ID("evt"),
				Type:        constants.EventJourneyReady,
				UserID:      held.UserID,
				AggregateID: held.ID,
				Data: map[string]any{
					"generation":   held.Generation,
					"journey_kind": held.Kind,
					"order_id":     held.SourceID,
					"sku_id":       held.SKUID,
					"rule_name":    rule.Name,
				},
			},
		)
	})
}
func (r *Repository) JourneyFeedback(ctx context.Context, id, user string, generation int64, delivered bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var j domain.Journey
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&j, "id = ? AND user_id = ? AND generation = ?", id, user, generation).Error; errors.Is(
			e,
			gorm.ErrRecordNotFound,
		) {
			return nil
		} else if e != nil {
			return e
		}
		now := time.Now().UTC()
		if delivered {
			if j.DeliveredAt == nil {
				j.DeliveredAt = &now
			}
			if j.Status == constants.JourneyStatusQueued {
				j.Status = constants.JourneyStatusDelivered
				j.Reason = "消息已写入顾客的站内消息中心"
			}
		} else if j.Status == constants.JourneyStatusQueued {
			j.Status = constants.JourneyStatusSuppressed
			j.Reason = "投递前检查未通过，未发送"
		}
		return tx.WithContext(ctx).Save(&j).Error
	})
}
func (r *Repository) JourneyClicked(ctx context.Context, j domain.Journey) error {
	return r.db.WithContext(ctx).
		Model(&domain.Journey{}).
		Where("id = ? AND user_id = ? AND clicked_at IS NULL", j.ID, j.UserID).
		Update("clicked_at", time.Now().UTC()).
		Error
}
func (r *Repository) CancelJourney(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).
		Model(&domain.Journey{}).
		Where("id = ? AND status IN ?", id, []string{constants.JourneyStatusWatching, constants.JourneyStatusQueued}).
		Updates(map[string]any{"status": constants.JourneyStatusExited, "reason": "后台取消触达"})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return status.Error(codes.FailedPrecondition, "此触达已完成或已退出")
	}
	return nil
}

func (r *Repository) JourneyPayment(
	ctx context.Context,
	event platform.Event,
	skus map[string]bool,
	at time.Time,
) error {
	ctx, sp := platform.Start(ctx, "engagement.repository.JourneyPayment")
	defer sp.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		owner, e := journeyOwner(ctx, tx, event.UserID)
		if e != nil {
			return e
		}
		res := tx.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&domain.JourneyReceipt{ID: event.ID})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		if at.After(owner.LastPaidAt) {
			owner.LastPaidAt = at
			if e = tx.WithContext(ctx).Save(&owner).Error; e != nil {
				return e
			}
		}
		// SQL bounds the source window; process records locally in batches without RPC.
		query := tx.WithContext(ctx).
			Where("user_id = ? AND source_at <= ? AND expires_at >= ? AND status IN ?", event.UserID, at, at, []string{constants.JourneyStatusWatching, constants.JourneyStatusQueued, constants.JourneyStatusDelivered})
		rows := []domain.Journey{}
		return query.Order("id").FindInBatches(&rows, 100, func(batch *gorm.DB, _ int) error {
			for _, j := range rows {
				match := j.Kind == constants.JourneyKindUnpaid && j.SourceID == event.AggregateID ||
					j.Kind == constants.JourneyKindRepurchase && skus[j.SKUID]
				if j.Kind == constants.JourneyKindCart {
					var saved []string
					_ = json.Unmarshal([]byte(j.SKUsJSON), &saved)
					if len(saved) == 0 {
						match = true
					} else {
						for _, sku := range saved {
							if skus[sku] {
								match = true
							}
						}
					}
				}
				if !match {
					continue
				}
				j.Status = constants.JourneyStatusExited
				j.Reason = "匹配商品已付款，自动退出"
				if j.ClickedAt != nil && !at.Before(*j.ClickedAt) && !at.After(j.ClickedAt.Add(7*24*time.Hour)) {
					j.Status = constants.JourneyStatusRecovered
					j.RecoveredAt = &at
					j.RecoveredOrderID = event.AggregateID
					j.Reason = "点击后7天内匹配商品付款，非因果归因"
				}
				if e = tx.WithContext(ctx).Save(&j).Error; e != nil {
					return e
				}
			}
			return nil
		}).Error
	})
}
