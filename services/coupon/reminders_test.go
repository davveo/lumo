package coupon_test

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/domain"
	"testing"
	"time"
)

func TestExpiryRemindersRequireOptInAndThrottleRealAvailableCoupons(t *testing.T) {
	db, m := setup(t, nil)
	now := time.Now().UTC()
	rows := []domain.Coupon{
		{ID: "soon", UserID: "buyer", State: "AVAILABLE", ExpiresAt: now.Add(time.Hour)},
		{ID: "used", UserID: "buyer", State: "USED", ExpiresAt: now.Add(time.Hour)},
		{ID: "late", UserID: "buyer", State: "AVAILABLE", ExpiresAt: now.Add(100 * time.Hour)},
	}
	if e := db.Create(&rows).Error; e != nil {
		t.Fatal(e)
	}
	if e := m.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	var count int64
	db.Table("outbox_events").Count(&count)
	if count != 0 {
		t.Fatal("unsubscribed reminder", count)
	}
	out := must(t, m, "reminder_update", "", "buyer", "user", map[string]any{"enabled": true})
	if platform.Int(out, "expiring_count") != 1 {
		t.Fatal(out)
	}
	if e := m.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := m.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	var p domain.ExpiryPreference
	if e := db.First(&p, "user_id = ?", "buyer").Error; e != nil || p.LastQueuedAt == nil {
		t.Fatal(p, e)
	}
	var events []platform.Outbox
	if e := db.Find(&events).Error; e != nil {
		t.Fatal(e)
	}
	if len(events) != 1 {
		t.Fatal(events)
	}
	must(t, m, "reminder_update", "", "buyer", "user", map[string]any{"enabled": false})
	p.LastQueuedAt = nil
	db.Model(&p).Update("last_queued_at", nil)
	if e := m.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	db.Find(&events)
	if len(events) != 1 {
		t.Fatal(events)
	}
}
