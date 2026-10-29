package adminquery

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"path/filepath"
	"testing"
	"time"
)

type fixture struct {
	ID        string `gorm:"primaryKey"`
	UserID    string
	Status    string
	Amount    int64
	CreatedAt time.Time
}

func TestManagementQueriesCountAllRowsAndRespectLiteralSearchAndBusinessDates(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "management.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	defer raw.Close()
	if err = db.AutoMigrate(&fixture{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []fixture{}
	for i := 0; i < 125; i++ {
		rows = append(
			rows,
			fixture{
				ID:        time.Unix(int64(i), 0).Format(time.RFC3339),
				UserID:    "ordinary",
				Status:    "PENDING",
				Amount:    100,
				CreatedAt: now,
			},
		)
	}
	rows = append(rows, fixture{ID: "literal%_", UserID: "buyer", Status: "SUCCEEDED", Amount: 875, CreatedAt: now})
	if err = db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	spec := Spec{State: "status", Amount: "amount", Search: []string{"id", "user_id"}}
	page, total, _, err := Page[fixture](ctx, db, map[string]any{"page": 7, "page_size": 20}, spec)
	if err != nil || total != 126 || len(page) != 6 {
		t.Fatalf("bounded server page: %d %d %v", len(page), total, err)
	}
	page, total, _, err = Page[fixture](ctx, db, map[string]any{"keyword": "%_"}, spec)
	if err != nil || total != 1 || page[0].ID != "literal%_" {
		t.Fatalf("keyword wildcards escaped: %+v %d %v", page, total, err)
	}
	value, err := Summary[fixture](ctx, db, map[string]any{"days": 7}, spec)
	if err != nil || value["total"] != int64(126) {
		t.Fatalf("summary must not count only one page: %v %v", value, err)
	}
	_, _, _, err = Page[fixture](ctx, db, map[string]any{"from": "2026-10-28", "to": "2026-10-27"}, spec)
	if err == nil {
		t.Fatal("inverted date range accepted")
	}
	if _, err = Summary[fixture](ctx, db, map[string]any{"days": 365}, spec); err == nil {
		t.Fatal("unbounded dashboard accepted")
	}
}
