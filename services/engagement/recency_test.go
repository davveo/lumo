package engagement_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement"
)

type recencyCatalog struct{}

func (recencyCatalog) Call(_ context.Context, _ string, c platform.Command) (platform.Result, error) {
	return platform.Result{"id": c.ID, "active": true}, nil
}

func TestRepeatedBrowsingPreservesCreationButRefreshesRecentHistory(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "engagement.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	m := engagement.New(platform.Dependencies{DB: db, RPC: recencyCatalog{}})
	if err = m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	put := func(id string) platform.Result {
		row, e := m.Handle(
			ctx,
			platform.Command{
				Action: "put",
				UserID: "reader",
				Role:   "user",
				Data:   map[string]any{"kind": "history", "sku_id": id},
			},
		)
		if e != nil {
			t.Fatal(e)
		}
		return row
	}
	old := put("old-interest")
	aged := time.Now().Add(-100 * 24 * time.Hour).Truncate(time.Second)
	if err = db.Table("engagement_records").Where("id = ?", old["id"]).Updates(
		map[string]any{"created_at": aged, "updated_at": aged},
	).Error; err != nil {
		t.Fatal(err)
	}
	put("new-interest")
	retouched := put("old-interest")
	updated, e := time.Parse(time.RFC3339, platform.Text(retouched, "updated_at"))
	if e != nil || updated.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("repeat browsing did not expose its recent touch", retouched)
	}
	created, e := time.Parse(time.RFC3339, platform.Text(retouched, "created_at"))
	if e != nil || !created.Equal(aged) {
		t.Fatal("repeat browsing overwrote original creation time", retouched)
	}
	list, err := m.Handle(
		ctx,
		platform.Command{
			Action: "list",
			UserID: "reader",
			Role:   "user",
			Data:   map[string]any{"kind": "history", "limit": 1},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Items []map[string]any `json:"items"`
	}
	if err = platform.Decode(list, &result); err != nil || len(result.Items) != 1 ||
		platform.Text(result.Items[0], "sku_id") != "old-interest" {
		t.Fatal("recent-history limit dropped a repeated old interest", list, err)
	}
}
