package analytics_test

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/domain"
	"testing"
	"time"
)

func TestCustomerSegmentsUseExactCustomerAndActualFinancialFacts(t *testing.T) {
	db, m := setup(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -45)
	facts := []domain.TransactionFact{
		{ID: "p1", UserID: "buyer", OrderID: "o1", Kind: "paid", AmountCents: 100000, Items: "[]", OccurredAt: old},
		{
			ID:          "r1",
			UserID:      "buyer",
			OrderID:     "o1",
			Kind:        "refund",
			AmountCents: 10000,
			Items:       "[]",
			OccurredAt:  old.Add(time.Hour),
		},
		{
			ID:          "p2",
			UserID:      "other",
			OrderID:     "o2",
			Kind:        "paid",
			AmountCents: 20000,
			Items:       "[]",
			OccurredAt:  time.Now().UTC(),
		},
	}
	if e := db.Create(&facts).Error; e != nil {
		t.Fatal(e)
	}
	query := func(data map[string]any) platform.Result {
		t.Helper()
		v, e := m.Operations["customer_stats"](ctx, platform.Request{UserID: "operator", Role: "admin", Data: data})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	out := query(map[string]any{"min_orders": 1, "min_net_cents": 50000, "inactive_days": 30})
	var rows struct {
		Items []struct {
			UserID string `json:"user_id"`
			Net    int64  `json:"net_cents"`
			Last   string `json:"last_paid_at"`
		} `json:"items"`
	}
	if e := platform.Decode(out, &rows); e != nil {
		t.Fatal(e)
	}
	if platform.Int(out, "total") != 1 || len(rows.Items) != 1 || rows.Items[0].UserID != "buyer" ||
		rows.Items[0].Net != 90000 {
		t.Fatal(out)
	}
	if _, e := time.Parse(time.RFC3339, rows.Items[0].Last); e != nil {
		t.Fatal(rows.Items[0].Last, e)
	}
	out = query(map[string]any{"customer_id": "other"})
	_ = platform.Decode(out, &rows)
	if len(rows.Items) != 1 || rows.Items[0].UserID != "other" {
		t.Fatal(out)
	}
}
