package repository

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"time"
)

func (r *Repository) Track(ctx context.Context, row *domain.Record, event *platform.Event, kind string) (bool, error) {
	inserted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if result.Error != nil {
			return result.Error
		}
		inserted = result.RowsAffected == 1
		if !inserted {
			return nil
		}
		fact := domain.BehaviorFact{ID: row.ID, UserID: row.UserID, Type: kind, OccurredAt: row.CreatedAt.UTC()}
		if err := tx.Create(&fact).Error; err != nil {
			return err
		}
		return platform.Enqueue(ctx, tx, *event)
	})
	return inserted, err
}
func (r *Repository) TransactionFact(ctx context.Context, row domain.TransactionFact) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}
func (r *Repository) Metrics(ctx context.Context, days int) (platform.Result, error) {
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -days+1).UTC()
	end := now.UTC()
	paid, refund, orders := int64(0), int64(0), int64(0)
	buyers := map[string]int{}
	stages := map[string]map[string]bool{"view": {}, "add_cart": {}, "checkout": {}, "paid": {}}
	trend := map[string]map[string]any{}
	products := map[string]map[string]any{}
	var financial []domain.TransactionFact
	err := r.db.WithContext(ctx).
		Where("occurred_at >= ? AND occurred_at <= ?", start, end).
		FindInBatches(&financial, 500, func(tx *gorm.DB, batch int) error {
			for _, f := range financial {
				day := f.OccurredAt.In(loc).Format("2006-01-02")
				d := trend[day]
				if d == nil {
					d = map[string]any{"day": day, "paid_cents": int64(0), "refund_cents": int64(0)}
					trend[day] = d
				}
				if f.Kind == "refund" {
					refund += f.AmountCents
					d["refund_cents"] = d["refund_cents"].(int64) + f.AmountCents
					continue
				}
				paid += f.AmountCents
				orders++
				buyers[f.UserID]++
				stages["paid"][f.UserID] = true
				d["paid_cents"] = d["paid_cents"].(int64) + f.AmountCents
				var items []map[string]any
				if err := json.Unmarshal([]byte(f.Items), &items); err != nil {
					return err
				}
				for _, item := range items {
					sku := platform.Text(item, "sku_id")
					if sku == "" {
						continue
					}
					p := products[sku]
					if p == nil {
						p = map[string]any{"sku_id": sku, "name": platform.Text(item, "name"), "quantity": int64(0)}
						products[sku] = p
					}
					p["quantity"] = p["quantity"].(int64) + platform.Int(item, "quantity")
				}
			}
			return nil
		}).
		Error
	if err != nil {
		return nil, err
	}
	var behaviors []domain.BehaviorFact
	err = r.db.WithContext(ctx).
		Where("occurred_at >= ? AND occurred_at <= ?", start, end).
		FindInBatches(&behaviors, 500, func(tx *gorm.DB, batch int) error {
			for _, f := range behaviors {
				if set := stages[f.Type]; set != nil {
					set[f.UserID] = true
				}
			}
			return nil
		}).
		Error
	if err != nil {
		return nil, err
	}
	repeat := 0
	for _, n := range buyers {
		if n >= 2 {
			repeat++
		}
	}
	funnel := []platform.Result{}
	cohort := map[string]bool{}
	for user := range stages["view"] {
		cohort[user] = true
	}
	for _, kind := range []string{"view", "add_cart", "checkout", "paid"} {
		for user := range cohort {
			if !stages[kind][user] {
				delete(cohort, user)
			}
		}
		funnel = append(funnel, platform.Result{"stage": kind, "users": len(stages[kind]), "cohort_users": len(cohort)})
	}
	daily := []map[string]any{}
	for i := 0; i < days; i++ {
		day := start.In(loc).AddDate(0, 0, i).Format("2006-01-02")
		row := trend[day]
		if row == nil {
			row = map[string]any{"day": day, "paid_cents": 0, "refund_cents": 0}
		}
		daily = append(daily, row)
	}
	top := []map[string]any{}
	for _, row := range products {
		top = append(top, row)
	}
	sort.Slice(top, func(i, j int) bool {
		a, b := top[i]["quantity"].(int64), top[j]["quantity"].(int64)
		if a == b {
			return top[i]["sku_id"].(string) < top[j]["sku_id"].(string)
		}
		return a > b
	})
	if len(top) > 10 {
		top = top[:10]
	}
	avg := int64(0)
	if orders > 0 {
		avg = paid / orders
	}
	repeatBPS := 0
	if len(buyers) > 0 {
		repeatBPS = repeat * 10000 / len(buyers)
	}
	var first domain.TransactionFact
	var since *time.Time
	result := r.db.WithContext(ctx).Order("occurred_at").Limit(1).Find(&first)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected > 0 {
		since = &first.OccurredAt
	}
	var history domain.MetricsHistory
	if err := r.db.WithContext(ctx).Where("id = ?", "history").Limit(1).Find(&history).Error; err != nil {
		return nil, err
	}
	return platform.Result{
		"days":                days,
		"timezone":            "Asia/Shanghai",
		"paid_cents":          paid,
		"refund_cents":        refund,
		"net_cents":           paid - refund,
		"paid_orders":         orders,
		"buyers":              len(buyers),
		"average_order_cents": avg,
		"repeat_buyers":       repeat,
		"repeat_purchase_bps": repeatBPS,
		"funnel":              funnel,
		"daily":               daily,
		"top_products":        top,
		"coverage_since":      since,
		"historical_backfill": history.PaymentComplete && history.RefundComplete,
		"funnel_scope": "期间登录用户的阶段到达人数；cohort_users为同时到达此前各" +
			"阶段的用户，不代表严格时间顺序转化",
	}, nil
}

func (r *Repository) HistoryFact(ctx context.Context, row domain.TransactionFact) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"occurred_at",
				"items",
				"amount_cents",
				"user_id",
				"order_id",
				"session_key",
				"source",
				"search_query",
				"campaign_cost",
				"coupon_cost",
				"points_cost",
				"cost_known",
			}),
		}).
		Create(&row).
		Error
}
func (r *Repository) MarkHistory(ctx context.Context, phase string) (bool, error) {
	done := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := domain.MetricsHistory{ID: "history"}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", row.ID).Error; e != nil {
			return e
		}
		if phase == "payment" {
			row.PaymentComplete = true
		} else {
			row.RefundComplete = true
		}
		done = row.PaymentComplete && row.RefundComplete
		if done {
			now := time.Now().UTC()
			row.CompletedAt = &now
		}
		return tx.Save(&row).Error
	})
	return done, err
}
