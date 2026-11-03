package repository

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/domain"
	"gorm.io/gorm"
	"math/big"
	"sort"
	"strings"
	"time"
)

type diagnosticRow struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	Views           int64  `json:"views"`
	Carts           int64  `json:"carts"`
	Searches        int64  `json:"searches"`
	ZeroResults     int64  `json:"zero_results"`
	PaidOrders      int64  `json:"paid_orders"`
	PaidCents       int64  `json:"paid_cents"`
	RefundCents     int64  `json:"refund_cents"`
	CampaignCost    int64  `json:"campaign_cost"`
	CouponCost      int64  `json:"coupon_cost"`
	PointsCost      int64  `json:"points_cost"`
	CostKnownOrders int64  `json:"cost_known_orders"`
}
type journey struct {
	Events []domain.JourneyEvent
	Paid   []time.Time
	User   string
}

func orderedStages(s journey) [4]bool {
	var out [4]bool
	stage := 0
	last := time.Time{}
	for _, e := range s.Events {
		want := []string{"view", "add_cart", "checkout"}
		if stage < 3 && e.Type == want[stage] && !e.OccurredAt.Before(last) {
			out[stage] = true
			last = e.OccurredAt
			stage++
		}
	}
	if stage == 3 {
		for _, at := range s.Paid {
			if !at.Before(last) {
				out[3] = true
				break
			}
		}
	}
	return out
}

func (r *Repository) Drilldown(
	ctx context.Context,
	days int,
	dimension, keyword string,
	page int,
) (platform.Result, error) {
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -days+1).UTC()
	end := now.UTC()
	rows := map[string]*diagnosticRow{}
	sessions := map[string]*journey{}
	get := func(key, name string) *diagnosticRow {
		if key == "" {
			key = "unknown"
		}
		if rows[key] == nil {
			rows[key] = &diagnosticRow{Key: key, Name: name}
		}
		return rows[key]
	}
	session := func(key, user string) *journey {
		k := key + ":" + user
		if sessions[k] == nil {
			sessions[k] = &journey{User: user}
		}
		return sessions[k]
	}
	var firstPaid []domain.TransactionFact
	// The earliest actual payment defines a new buyer, including transactions
	// preceding the selected window. Equal timestamps have an ID tie-breaker.
	e := r.db.WithContext(ctx).
		Table("transaction_facts AS f").
		Select("f.id,f.user_id,f.order_id,f.occurred_at").
		Where("f.kind = ? AND f.occurred_at <= ?", "paid", end).
		Where("NOT EXISTS (SELECT 1 FROM transaction_facts AS p WHERE p.kind = ? AND p.user_id "+
			"= f.user_id AND (p.occurred_at < f.occurred_at OR (p.occurred_at = "+
			"f.occurred_at AND p.id < f.id)))", "paid").
		Find(&firstPaid).
		Error
	if e != nil {
		return nil, e
	}
	newIDs := map[string]bool{}
	newUsers := map[string]bool{}
	for _, f := range firstPaid {
		newIDs[f.ID] = true
		newIDs["refund-order:"+f.OrderID] = true
		if !f.OccurredAt.Before(start) {
			newUsers[f.UserID] = true
		}
	}
	var paidUsers []string
	if e = r.db.WithContext(ctx).Model(&domain.TransactionFact{}).Where("kind = ? AND occurred_at <= ?", "paid", end).Distinct().Pluck("user_id", &paidUsers).Error; e != nil {
		return nil, e
	}
	knownBuyer := map[string]bool{}
	for _, id := range paidUsers {
		knownBuyer[id] = true
	}
	financial := []domain.TransactionFact{}
	paid, attributed, costKnown := int64(0), int64(0), int64(0)
	campaign, coupon, points := int64(0), int64(0), int64(0)
	e = r.db.WithContext(ctx).
		Where("occurred_at >= ? AND occurred_at <= ?", start, end).
		FindInBatches(&financial, 500, func(_ *gorm.DB, _ int) error {
			for _, f := range financial {
				if f.Kind == "paid" {
					paid++
					if f.SessionKey != "" {
						attributed++
						session(f.SessionKey, f.UserID).Paid = append(
							session(f.SessionKey, f.UserID).Paid,
							f.OccurredAt,
						)
					}
					if f.CostKnown {
						costKnown++
						campaign += f.CampaignCost
						coupon += f.CouponCost
						points += f.PointsCost
					}
				}
				key, name := "unknown", "未记录"
				switch dimension {
				case "source":
					key = f.Source
					name = key
				case "query":
					key = f.SearchQuery
					name = key
				case "customer":
					if f.UserID == "" {
						key = "unknown"
					} else if newIDs[f.ID] || f.Kind == "refund" && newIDs["refund-order:"+f.OrderID] {
						key = "new"
					} else {
						key = "returning"
					}
					name = key
				}
				if dimension != "product" {
					d := get(key, name)
					applyFinancial(d, f, 1, 1)
					continue
				}
				var items []map[string]any
				_ = json.Unmarshal([]byte(f.Items), &items)
				weights := map[string]int64{}
				names := map[string]string{}
				total := int64(0)
				for _, item := range items {
					sku := platform.Text(item, "sku_id")
					if sku == "" {
						continue
					}
					weight := platform.Int(item, "amount_cents")
					if weight <= 0 {
						weight = platform.Int(item, "unit_price_cents") * platform.Int(item, "quantity")
					}
					if weight <= 0 {
						weight = platform.Int(item, "quantity")
					}
					if weight <= 0 {
						weight = 1
					}
					weights[sku] += weight
					total += weight
					names[sku] = platform.Text(item, "name")
				}
				if len(weights) == 0 {
					applyFinancial(get("unknown", "未记录商品"), f, 1, 1)
				}
				keys := []string{}
				for sku := range weights {
					keys = append(keys, sku)
				}
				sort.Strings(keys)
				// Cumulative allocation preserves every cent across multiple product lines.
				previous := int64(0)
				for _, sku := range keys {
					next := previous + weights[sku]
					d := get(sku, names[sku])
					applyFinancialRange(d, f, previous, next, total)
					previous = next
				}
			}
			return nil
		}).
		Error
	if e != nil {
		return nil, e
	}
	events := []domain.JourneyEvent{}
	anonymous, logged, searches, zeros := int64(0), int64(0), int64(0), int64(0)
	e = r.db.WithContext(ctx).
		Where("occurred_at >= ? AND occurred_at <= ?", start, end).
		FindInBatches(&events, 500, func(_ *gorm.DB, _ int) error {
			for _, event := range events {
				s := session(event.SessionKey, event.UserID)
				s.Events = append(s.Events, event)
				if event.UserID == "" {
					anonymous++
				} else {
					logged++
				}
				if event.Type == "search" {
					searches++
					if event.ResultCount == 0 {
						zeros++
					}
				}
				key, name := event.SKU, event.SKU
				switch dimension {
				case "source":
					key = event.Source
					name = key
				case "query":
					key = event.Query
					name = key
				case "customer":
					if event.UserID == "" {
						key = "anonymous"
					} else if newUsers[event.UserID] {
						key = "new"
					} else if knownBuyer[event.UserID] {
						key = "returning"
					} else {
						key = "unpaid"
					}
					name = key
				}
				d := get(key, name)
				switch event.Type {
				case "view":
					d.Views++
				case "add_cart":
					d.Carts++
				case "search":
					d.Searches++
					if event.ResultCount == 0 {
						d.ZeroResults++
					}
				}
			}
			return nil
		}).
		Error
	if e != nil {
		return nil, e
	}
	funnel := [4]int{}
	anonymousSessions, loggedSessions, abandoned, cartSessions, immature := 0, 0, 0, 0, 0
	for _, s := range sessions {
		if len(s.Events) == 0 {
			continue
		}
		if s.User == "" {
			anonymousSessions++
		} else {
			loggedSessions++
		}
		sort.Slice(s.Events, func(i, j int) bool {
			if s.Events[i].OccurredAt.Equal(s.Events[j].OccurredAt) {
				return s.Events[i].ID < s.Events[j].ID
			}
			return s.Events[i].OccurredAt.Before(s.Events[j].OccurredAt)
		})
		stages := orderedStages(*s)
		for i, done := range stages {
			if done {
				funnel[i]++
			}
		}
		var cartAt time.Time
		for _, event := range s.Events {
			if event.Type == "add_cart" && (cartAt.IsZero() || event.OccurredAt.After(cartAt)) {
				cartAt = event.OccurredAt
			}
		}
		if !cartAt.IsZero() {
			if cartAt.After(end.Add(-24 * time.Hour)) {
				immature++
			} else {
				cartSessions++
				paidAfter := false
				for _, at := range s.Paid {
					if !at.Before(cartAt) {
						paidAfter = true
					}
				}
				if !paidAfter {
					abandoned++
				}
			}
		}
	}
	items := []diagnosticRow{}
	for _, row := range rows {
		if keyword == "" || strings.Contains(strings.ToLower(row.Key+" "+row.Name), strings.ToLower(keyword)) {
			items = append(items, *row)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.PaidCents != b.PaidCents {
			return a.PaidCents > b.PaidCents
		}
		if a.Views+a.Searches != b.Views+b.Searches {
			return a.Views+a.Searches > b.Views+b.Searches
		}
		return a.Key < b.Key
	})
	total := len(items)
	lo := min(total, (page-1)*20)
	hi := min(total, lo+20)
	var first domain.JourneyEvent
	result := r.db.WithContext(ctx).Order("occurred_at,id").Limit(1).Find(&first)
	if result.Error != nil {
		return nil, result.Error
	}
	var since any
	if result.RowsAffected > 0 {
		since = first.OccurredAt
	}
	return platform.Result{
		"days":                       days,
		"dimension":                  dimension,
		"items":                      items[lo:hi],
		"total":                      total,
		"page":                       page,
		"page_size":                  20,
		"coverage_since":             since,
		"anonymous_events":           anonymous,
		"logged_events":              logged,
		"anonymous_sessions":         anonymousSessions,
		"logged_sessions":            loggedSessions,
		"paid_orders":                paid,
		"attributed_orders":          attributed,
		"missing_attribution_orders": paid - attributed,
		"cost_known_orders":          costKnown,
		"campaign_cost":              campaign,
		"coupon_cost":                coupon,
		"points_cost":                points,
		"searches":                   searches,
		"zero_results":               zeros,
		"ordered_funnel": []platform.Result{
			{"stage": "view", "sessions": funnel[0]},
			{"stage": "add_cart", "sessions": funnel[1]},
			{"stage": "checkout", "sessions": funnel[2]},
			{"stage": "paid", "sessions": funnel[3]},
		},
		"cart_sessions_observed_24h": cartSessions,
		"abandoned_cart_sessions":    abandoned,
		"immature_cart_sessions":     immature,
		"scope": "行为来自浏览器上报，按服务端接收时间统计；漏斗要求同" +
			"一会话和身份依次浏览、加购、结算、支付，登录会切换会" +
			"话。弃购为加购后观察满24小时仍未记录同会话支付，不代表" +
			"永久流失。新客为首次实际支付，历史未同步前可能不完整" +
			"。优惠成本为已付款订单中已记录的活动、券、积分优惠毛" +
			"额，未扣退款；商品金额和优惠按商品金额比例分摊。未知" +
			"归因保留，不推算利润或活动增量。",
	}, nil
}
func applyFinancial(d *diagnosticRow, f domain.TransactionFact, n, total int64) {
	applyFinancialRange(d, f, 0, n, total)
}
func applyFinancialRange(d *diagnosticRow, f domain.TransactionFact, lo, hi, total int64) {
	if total <= 0 {
		return
	}
	part := func(v int64) int64 {
		var a, b big.Int
		a.Mul(big.NewInt(v), big.NewInt(hi))
		a.Quo(&a, big.NewInt(total))
		b.Mul(big.NewInt(v), big.NewInt(lo))
		b.Quo(&b, big.NewInt(total))
		return a.Sub(&a, &b).Int64()
	}
	if f.Kind == "refund" {
		d.RefundCents += part(f.AmountCents)
		return
	}
	d.PaidOrders++
	d.PaidCents += part(f.AmountCents)
	if f.CostKnown {
		d.CostKnownOrders++
		d.CampaignCost += part(f.CampaignCost)
		d.CouponCost += part(f.CouponCost)
		d.PointsCost += part(f.PointsCost)
	}
}
