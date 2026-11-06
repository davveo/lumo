package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/reconciliation/internal/domain"
)

func TestUnclaimedRunHasNullLeaseAndCanBeClaimed(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "lease.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	repo := New(db)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	row := domain.Run{
		ID:      "lease-test-run",
		ActorID: "admin",
		From:    "2026-11-01",
		To:      "2026-11-04",
		Reason:  "lease regression",
		Status:  constants.ReconciliationStatusQueued,
	}
	for range 2 {
		if _, err := repo.Start(ctx, &row); err != nil {
			t.Fatal(err)
		}
	}
	var unclaimed int64
	if err := db.Model(&domain.Run{}).Where("lease_until IS NULL").Count(&unclaimed).Error; err != nil {
		t.Fatal(err)
	}
	if unclaimed != 1 {
		t.Fatalf("unclaimed job must use SQL NULL rather than a MySQL zero date: %d", unclaimed)
	}
	job, err := repo.Claim(ctx)
	if err != nil || job == nil {
		t.Fatalf("unclaimed job could not be leased: %v", err)
	}
	var saved domain.Run
	if err := db.First(&saved, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.LeaseUntil == nil || !saved.LeaseUntil.After(time.Now()) || saved.Claim == "" {
		t.Fatal("claim did not establish a live lease")
	}
}
