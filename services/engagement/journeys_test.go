package engagement_test

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement"
	"github.com/davveo/lumo/services/engagement/internal/domain"
	"github.com/davveo/lumo/services/notification"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type journeyPeer struct {
	opted       bool
	match       bool
	cartEmpty   bool
	orderStatus string
	engagement  platform.Module
	calls       map[string]int
}

func (p *journeyPeer) Call(ctx context.Context, service string, c platform.Command) (platform.Result, error) {
	p.calls[service+"/"+c.Action]++
	switch service {
	case "engagement":
		return p.engagement.Operations[c.Action](ctx, c.Request())
	case "iam":
		return platform.Result{"id": c.ID, "role": "user", "active": true}, nil
	case "member":
		if c.Action == "segment_match" {
			return platform.Result{"matches": p.match, "valid": true}, nil
		}
		return platform.Result{
			"preferences": map[string]any{"notifications": map[string]any{"promotion": p.opted}},
		}, nil
	case "cart":
		rows := []platform.Result{}
		if !p.cartEmpty {
			rows = append(
				rows,
				platform.Result{
					"sku_id":     "coffee",
					"quantity":   1,
					"updated_at": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano),
				},
			)
		}
		return platform.Result{"items": rows}, nil
	case "order":
		return platform.Result{
			"id":         c.ID,
			"user_id":    "buyer",
			"status":     p.orderStatus,
			"created_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			"updated_at": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"items":      []platform.Result{{"sku_id": "coffee", "quantity": 1}},
		}, nil
	case "catalog":
		return platform.Result{"id": c.ID, "active": true, "product_id": "coffee"}, nil
	case "inventory":
		return platform.Result{"items": []platform.Result{{"sku_id": "coffee", "available": 10}}}, nil
	case "pricing":
		return platform.Result{"quote": platform.Result{"payable_cents": 1000}}, nil
	}
	return nil, status.Error(codes.Unimplemented, "unexpected peer")
}
func journeyDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), name+".db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	if e = platform.MigratePlatform(ctx, db); e != nil {
		t.Fatal(e)
	}
	return db
}
func journeySetup(t *testing.T) (platform.Module, *gorm.DB, *journeyPeer) {
	db := journeyDB(t, "journeys")
	p := &journeyPeer{match: true, orderStatus: constants.OrderStatusPendingPayment, calls: map[string]int{}}
	m := engagement.New(platform.Dependencies{DB: db, RPC: p})
	p.engagement = m
	if e := m.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	return m, db, p
}
func saveJourneyRule(t *testing.T, m platform.Module, kind string) string {
	t.Helper()
	r, e := m.Operations["journey_rule_save"](
		context.Background(),
		platform.Request{
			ID:     "rule-" + kind,
			UserID: "admin",
			Role:   "admin",
			Data: map[string]any{
				"name":          "触达验收",
				"kind":          kind,
				"enabled":       true,
				"delay_minutes": 1,
				"cycle_days":    1,
				"gap_hours":     24,
				"sku_id":        "coffee",
			},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	return platform.Text(r, "id")
}
func cartJourneyEvent(at time.Time, id string) platform.Event {
	return platform.Event{
		ID:          id,
		Type:        constants.EventCartChanged,
		UserID:      "buyer",
		AggregateID: "buyer",
		Data:        map[string]any{"changed_at": at.UTC().Format(time.RFC3339Nano)},
	}
}
func TestJourneysRequireOptInAndRecordActualDeliveryClickAndMatchingPayment(t *testing.T) {
	ctx := context.Background()
	m, db, p := journeySetup(t)
	saveJourneyRule(t, m, constants.JourneyKindCart)
	if e := m.OnEvent(ctx, cartJourneyEvent(time.Now().Add(-48*time.Hour), "cart-old")); e != nil {
		t.Fatal(e)
	}
	if e := m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var suppressed int64
	db.Model(&domain.Journey{}).Where("status = ?", constants.JourneyStatusSuppressed).Count(&suppressed)
	if suppressed != 1 {
		t.Fatal("missing opt-in was not suppressed")
	}
	p.opted = true
	if e := m.OnEvent(ctx, cartJourneyEvent(time.Now().Add(-36*time.Hour), "cart-new")); e != nil {
		t.Fatal(e)
	}
	if e := m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var queued domain.Journey
	if e := db.First(&queued, "status = ?", constants.JourneyStatusQueued).Error; e != nil {
		t.Fatal(e)
	}
	if queued.DeliveredAt != nil {
		t.Fatal("queued work was counted as delivered")
	}
	var out platform.Outbox
	if e := db.First(&out, "event_type = ?", constants.EventJourneyReady).Error; e != nil {
		t.Fatal(e)
	}
	var event platform.Event
	_ = json.Unmarshal([]byte(out.PayloadJSON), &event)
	notificationDB := journeyDB(t, "notifications")
	notifier := notification.New(platform.Dependencies{DB: notificationDB, RPC: p})
	if e := notifier.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e := notifier.OnEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	if e := notifier.OnEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	var feedback platform.Outbox
	if e := notificationDB.First(&feedback, "event_type = ?", constants.EventJourneyDelivered).Error; e != nil {
		t.Fatal(e)
	}
	_ = json.Unmarshal([]byte(feedback.PayloadJSON), &event)
	if e := m.OnEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	var inbox int64
	notificationDB.Table("notification_records").Where("kind = ?", "inbox").Count(&inbox)
	if inbox != 1 {
		t.Fatal("duplicate inbox messages", inbox)
	}
	if _, e := m.Operations["journey_open"](ctx, platform.Request{ID: queued.ID, UserID: "other", Role: "user"}); status.Code(
		e,
	) != codes.PermissionDenied {
		t.Fatal("foreign journey opened", e)
	}
	opened, e := m.Operations["journey_open"](ctx, platform.Request{ID: queued.ID, UserID: "buyer", Role: "user"})
	if e != nil || platform.Text(opened, "target") != "#/cart" || p.calls["inventory/batch_get"] != 1 ||
		p.calls["pricing/optimize"] != 1 {
		t.Fatal("recovery failed to revalidate current quote and stock", opened, e, p.calls)
	}
	if e = m.OnEvent(ctx, cartJourneyEvent(time.Now().Add(-24*time.Hour), "cart-third")); e != nil {
		t.Fatal(e)
	}
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var waiting domain.Journey
	if e = db.First(&waiting, "status = ?", constants.JourneyStatusWatching).Error; e != nil ||
		!strings.Contains(waiting.Reason, "频控") {
		t.Fatal("cross-rule frequency was ignored", waiting, e)
	}
	paid := platform.Event{
		ID:          "paid",
		Type:        constants.EventOrderPaymentAccepted,
		UserID:      "buyer",
		AggregateID: "new-paid-order",
		Data: map[string]any{
			"occurred_at": time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano),
			"items":       []platform.Result{{"sku_id": "coffee", "quantity": 1}},
		},
	}
	if e = m.OnEvent(ctx, paid); e != nil {
		t.Fatal(e)
	}
	if e = m.OnEvent(ctx, paid); e != nil {
		t.Fatal(e)
	}
	var recovered int64
	db.Model(&domain.Journey{}).Where("status = ?", constants.JourneyStatusRecovered).Count(&recovered)
	if recovered != 1 {
		t.Fatal("matching clicked payment not recorded", recovered)
	}
}
func TestJourneyRechecksPreferenceBeforeInboxAndExitsPaidOrders(t *testing.T) {
	ctx := context.Background()
	m, db, p := journeySetup(t)
	p.opted = true
	saveJourneyRule(t, m, constants.JourneyKindCart)
	_ = m.OnEvent(ctx, cartJourneyEvent(time.Now().Add(-time.Hour), "cart"))
	if e := m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var row platform.Outbox
	db.First(&row, "event_type = ?", constants.EventJourneyReady)
	var ready platform.Event
	_ = json.Unmarshal([]byte(row.PayloadJSON), &ready)
	p.opted = false
	ndb := journeyDB(t, "suppressed")
	notifier := notification.New(platform.Dependencies{DB: ndb, RPC: p})
	if e := notifier.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e := notifier.OnEvent(ctx, ready); e != nil {
		t.Fatal(e)
	}
	var count int64
	ndb.Table("notification_records").Where("kind = ?", "inbox").Count(&count)
	if count != 0 {
		t.Fatal("preference revocation still delivered")
	}
	var feedback platform.Outbox
	if e := ndb.First(&feedback, "event_type = ?", constants.EventJourneySuppressed).Error; e != nil {
		t.Fatal(e)
	}
	p.opted = true
	ruleID := saveJourneyRule(t, m, constants.JourneyKindUnpaid)
	_, e := m.Operations["journey_run"](
		ctx,
		platform.Request{
			ID:     ruleID,
			UserID: "admin",
			Role:   "admin",
			Data:   map[string]any{"customer_id": "buyer", "order_id": "unpaid-order"},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	p.orderStatus = constants.OrderStatusReadyToFulfill
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var j domain.Journey
	if e = db.First(&j, "kind = ?", constants.JourneyKindUnpaid).Error; e != nil ||
		j.Status != constants.JourneyStatusSuppressed {
		t.Fatal("already-paid source was sent", j, e)
	}
}
func TestRepurchaseUsesActualCompletedProductAndConfiguredCycle(t *testing.T) {
	ctx := context.Background()
	m, db, p := journeySetup(t)
	p.opted = true
	p.orderStatus = constants.OrderStatusCompleted
	saveJourneyRule(t, m, constants.JourneyKindRepurchase)
	event := platform.Event{
		ID:          "complete",
		Type:        constants.EventOrderCompleted,
		UserID:      "buyer",
		AggregateID: "completed-order",
	}
	if e := m.OnEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	if e := m.OnEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	var n int64
	db.Model(&domain.Journey{}).Count(&n)
	if n != 1 {
		t.Fatal("completion retry created duplicates", n)
	}
	if e := m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var j domain.Journey
	db.First(&j)
	if j.Kind != constants.JourneyKindRepurchase || j.SKUID != "coffee" || j.Status != constants.JourneyStatusQueued ||
		j.DueAt.Sub(j.SourceAt) != 24*time.Hour {
		t.Fatal(j)
	}
}
