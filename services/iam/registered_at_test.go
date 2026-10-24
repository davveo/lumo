package iam_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	service "github.com/davveo/lumo/services/iam"
)

func TestSessionProfileUsesOriginalAccountRegistrationTime(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "iam.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sql.Close() })
	module := service.New(platform.Dependencies{DB: db})
	if err = module.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = module.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	registered := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	if err = db.Table("iam_accounts").Where("id = ?", "demo").Update("created_at", registered).Error; err != nil {
		t.Fatal(err)
	}
	invoke := func(action string, data map[string]any) platform.Result {
		t.Helper()
		result, e := module.Handle(ctx, platform.Command{Action: action, Data: data})
		if e != nil {
			t.Fatalf("%s failed: %v", action, e)
		}
		if result["registered_at"] != registered.Format(time.RFC3339) {
			t.Fatalf("%s used session/profile time instead of account time", action)
		}
		return result
	}
	login := invoke("login", map[string]any{"username": "demo", "password": "Lumo@2026"})
	invoke("validate", map[string]any{"token": login["token"], "registered_at": "2099-01-01T00:00:00Z"})
	refreshed := invoke("refresh", map[string]any{"token": login["token"]})
	invoke("validate", map[string]any{"token": refreshed["token"]})
	// Reseeding changes no registered-at facts for existing users.
	if err = module.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	invoke("validate", map[string]any{"token": refreshed["token"]})
}
