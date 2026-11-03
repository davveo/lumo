package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

type customerTotals struct {
	UserID      string `json:"user_id"`
	PaidOrders  int64  `json:"paid_orders"`
	PaidCents   int64  `json:"paid_cents"`
	RefundCents int64  `json:"refund_cents"`
	NetCents    int64  `json:"net_cents"`
	LastPaidAt  string `json:"last_paid_at"`
}

func (r *Repository) CustomerStats(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "analytics.repository.CustomerStats")
	defer sp.End()
	page, size := int(platform.Int(c.Data, "page")), int(platform.Int(c.Data, "page_size"))
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	minOrders, minNet, inactive := platform.Int(
		c.Data,
		"min_orders",
	), platform.Int(
		c.Data,
		"min_net_cents",
	), platform.Int(
		c.Data,
		"inactive_days",
	)
	customer := platform.Text(c.Data, "customer_id")
	if page < 1 || page > 100000 || size < 1 || size > 100 || minOrders < 0 || minOrders > 1000000 || minNet < 0 ||
		minNet > 100000000000000 ||
		inactive < 0 ||
		inactive > 3650 ||
		len(customer) > 128 {
		return nil, status.Error(codes.InvalidArgument, "invalid customer statistics filter")
	}
	if customer == "" && minOrders == 0 {
		minOrders = 1
	}
	base := r.db.WithContext(ctx).
		Model(&domain.TransactionFact{}).
		Where("user_id <> ''").
		Select("user_id, COUNT(DISTINCT CASE WHEN kind = 'paid' THEN order_id END) AS paid_orders, SUM(CASE WHEN kind = 'paid' THEN amount_cents ELSE 0 END) AS paid_cents, SUM(CASE WHEN kind = 'refund' THEN amount_cents ELSE 0 END) AS refund_cents, SUM(CASE WHEN kind = 'paid' THEN amount_cents ELSE -amount_cents END) AS net_cents, COALESCE(MAX(CASE WHEN kind = 'paid' THEN occurred_at END),'') AS last_paid_at").
		Group("user_id")
	if customer != "" {
		base = base.Where("user_id = ?", customer)
	}
	q := r.db.WithContext(ctx).Table("(?) AS customer_totals", base)
	if customer == "" {
		q = q.Where("paid_orders >= ? AND net_cents >= ?", minOrders, minNet)
	}
	if inactive > 0 {
		q = q.Where("last_paid_at <> '' AND last_paid_at <= ?", time.Now().UTC().AddDate(0, 0, -int(inactive)))
	}
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	rows := []customerTotals{}
	if e := q.Order("user_id ASC").Limit(size).Offset((page - 1) * size).Scan(&rows).Error; e != nil {
		return nil, e
	}
	for i := range rows {
		rows[i].LastPaidAt = customerTime(rows[i].LastPaidAt)
	}
	var coverage struct{ Since *string }
	if e := r.db.WithContext(ctx).Model(&domain.TransactionFact{}).Select("MIN(occurred_at) AS since").Scan(&coverage).Error; e != nil {
		return nil, e
	}
	since := ""
	if coverage.Since != nil {
		since = customerTime(*coverage.Since)
	}
	return platform.Result{
		"items":          rows,
		"total":          total,
		"page":           page,
		"page_size":      size,
		"generated_at":   time.Now().UTC(),
		"coverage_since": since,
		"scope":          "已采集的实际付款与退款事实；净实付=实付-退款。分群只包含有付款记录的顾客，历史未同步时不代表完整历史。",
	}, nil
}
func customerTime(raw string) string {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05.999999999"} {
		if v, e := time.Parse(layout, raw); e == nil {
			return v.UTC().Format(time.RFC3339)
		}
	}
	return raw
}
