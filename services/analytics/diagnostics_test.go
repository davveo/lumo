package analytics_test

import (
	"context"
	"github.com/davveo/lumo/internal/attribution"
	"github.com/davveo/lumo/internal/platform"
	analyticsmod "github.com/davveo/lumo/services/analytics"
	"github.com/davveo/lumo/services/analytics/internal/domain"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

func setup(t *testing.T) (*gorm.DB, platform.Module) {
	t.Helper()
	db, e := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	m := analyticsmod.New(platform.Dependencies{DB: db})
	if e = m.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	return db, m
}
func TestAnonymousTrackScrubsPIIAndDoesNotAcceptClaimedUser(t *testing.T) {
	db, m := setup(t)
	c := platform.Request{
		IdempotencyKey: "unique-behavior",
		Data: map[string]any{
			"session_id":   "session-1234567890",
			"visitor_id":   "visitor-1234567890",
			"user_id":      "victim",
			"type":         "search",
			"source":       "unknown-external",
			"query":        "冰箱 test@example.com 13812345678",
			"result_count": 0,
		},
	}
	if _, e := m.Operations["track"](context.Background(), c); e != nil {
		t.Fatal(e)
	}
	if out, e := m.Operations["track"](context.Background(), c); e != nil || out["duplicate"] != true {
		t.Fatal(out, e)
	}
	var rows []domain.JourneyEvent
	if e := db.Find(&rows).Error; e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	if rows[0].UserID != "" || rows[0].Query != "冰箱" || rows[0].Source != "unknown" {
		t.Fatal(rows)
	}
	out, e := m.Operations["drilldown"](
		context.Background(),
		platform.Request{UserID: "admin", Role: "admin", Data: map[string]any{"days": 7, "dimension": "query"}},
	)
	if e != nil {
		t.Fatal(e)
	}
	if platform.Int(out, "zero_results") != 1 || platform.Int(out, "anonymous_events") != 1 {
		t.Fatal(out)
	}
}
func TestOrderedFunnelRejectsOutOfOrderStagesAndCostsPreserveCents(t *testing.T) {
	db, m := setup(t)
	ctx := attribution.Context{SessionID: "session-1234567890", VisitorID: "visitor-1234567890", Source: "search"}
	base := time.Now().UTC().Add(-48 * time.Hour)
	rows := []domain.JourneyEvent{
		{ID: "e-z-view", SessionKey: ctx.SessionKey(), UserID: "buyer", Type: "view", SKU: "sku-1", OccurredAt: base},
		{
			ID:         "e-a-cart",
			SessionKey: ctx.SessionKey(),
			UserID:     "buyer",
			Type:       "add_cart",
			SKU:        "sku-1",
			OccurredAt: base.Add(time.Minute),
		},
		{
			ID:         "e-b-checkout",
			SessionKey: ctx.SessionKey(),
			UserID:     "buyer",
			Type:       "checkout",
			OccurredAt: base.Add(2 * time.Minute),
		},
		{ID: "e-wrong-cart", SessionKey: "out-of-order", Type: "add_cart", SKU: "sku-2", OccurredAt: base},
		{
			ID:         "e-wrong-view",
			SessionKey: "out-of-order",
			Type:       "view",
			SKU:        "sku-2",
			OccurredAt: base.Add(time.Minute),
		},
	}
	if e := db.Create(&rows).Error; e != nil {
		t.Fatal(e)
	}
	facts := []domain.TransactionFact{
		{
			ID:          "payment:old",
			OrderID:     "old",
			UserID:      "buyer",
			Kind:        "paid",
			AmountCents: 1,
			OccurredAt:  base.Add(-100 * 24 * time.Hour),
			Items:       "[]",
		},
		{
			ID:           "payment:one",
			OrderID:      "one",
			UserID:       "buyer",
			Kind:         "paid",
			AmountCents:  10,
			OccurredAt:   base.Add(3 * time.Minute),
			SessionKey:   ctx.SessionKey(),
			Source:       "search",
			CostKnown:    true,
			CampaignCost: 3,
			CouponCost:   2,
			PointsCost:   1,
			Items:        `[{"sku_id":"sku-1","amount_cents":1},{"sku_id":"sku-2","amount_cents":2}]`,
		},
		{
			ID:          "payment:new",
			OrderID:     "new",
			UserID:      "newbuyer",
			Kind:        "paid",
			AmountCents: 5,
			OccurredAt:  base,
			Items:       "[]",
		},
	}
	if e := db.Create(&facts).Error; e != nil {
		t.Fatal(e)
	}
	read := func(dimension string) platform.Result {
		out, e := m.Operations["drilldown"](
			context.Background(),
			platform.Request{UserID: "admin", Role: "admin", Data: map[string]any{"days": 7, "dimension": dimension}},
		)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	out := read("product")
	var response struct {
		Items []struct {
			Paid     int64 `json:"paid_cents"`
			Campaign int64 `json:"campaign_cost"`
		}
		Funnel []struct {
			Stage    string
			Sessions int
		} `json:"ordered_funnel"`
	}
	_ = platform.Decode(out, &response)
	var paid, cost int64
	for _, r := range response.Items {
		paid += r.Paid
		cost += r.Campaign
	}
	if paid != 15 || cost != 3 || response.Funnel[1].Sessions != 1 || response.Funnel[3].Sessions != 1 ||
		platform.Int(out, "abandoned_cart_sessions") != 1 ||
		platform.Int(out, "missing_attribution_orders") != 1 {
		t.Fatal(out)
	}
	customers := read("customer")
	var rowsOut struct {
		Items []struct {
			Key  string
			Paid int64 `json:"paid_orders"`
		}
	}
	_ = platform.Decode(customers, &rowsOut)
	counts := map[string]int64{}
	for _, r := range rowsOut.Items {
		counts[r.Key] = r.Paid
	}
	if counts["new"] != 1 || counts["returning"] != 1 {
		t.Fatal(customers)
	}
}
