package repository

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/domain"
	"gorm.io/gorm"
	"time"
)

func (r *Repository) SkuSales(ctx context.Context, ids []string, days int) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "analytics.repository.SkuSales")
	defer sp.End()
	quantity := map[string]int64{}
	for _, id := range ids {
		quantity[id] = 0
	}
	seen := map[string]bool{}
	var rows []domain.TransactionFact
	e := r.db.WithContext(ctx).
		Where("kind = ? AND occurred_at >= ?", "paid", time.Now().UTC().AddDate(0, 0, -days)).
		FindInBatches(&rows, 500, func(tx *gorm.DB, batch int) error {
			for _, fact := range rows {
				if seen[fact.OrderID] {
					continue
				}
				seen[fact.OrderID] = true
				var items []platform.Result
				if e := json.Unmarshal([]byte(fact.Items), &items); e != nil {
					return e
				}
				for _, item := range items {
					id := platform.Text(item, "sku_id")
					if _, ok := quantity[id]; ok {
						quantity[id] += max(int64(0), platform.Int(item, "quantity"))
					}
				}
			}
			return nil
		}).
		Error
	if e != nil {
		return nil, e
	}
	items := []platform.Result{}
	for _, id := range ids {
		items = append(items, platform.Result{"sku_id": id, "quantity": quantity[id]})
	}
	var history domain.MetricsHistory
	if e = r.db.WithContext(ctx).Limit(1).Find(&history).Error; e != nil {
		return nil, e
	}
	var first domain.TransactionFact
	if e = r.db.WithContext(ctx).Where("kind = ?", "paid").Order("occurred_at ASC,id ASC").Limit(1).Find(&first).Error; e != nil {
		return nil, e
	}
	ready := history.PaymentComplete && !first.OccurredAt.IsZero() &&
		first.OccurredAt.Before(time.Now().UTC().AddDate(0, 0, -7))
	return platform.Result{
		"items":          items,
		"days":           days,
		"forecast_ready": ready,
		"scope":          "按已采集支付订单去重的商品件数，未扣退货；历史未同步完整时只使用库存阈值补货。",
	}, nil
}
