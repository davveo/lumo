package repository

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccountAvailabilityRevokesSessionsWithoutExposingCredentials(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "iam.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	defer raw.Close()
	repo := New(db)
	if err = repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, a := range []domain.Account{{ID: "buyer", Username: "buyer", Role: "user", Active: true, PasswordHash: "private-hash"}, {ID: "admin", Username: "admin", Role: "admin", Active: true}} {
		if err = db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Create(&domain.Session{Digest: "private-session", UserID: "buyer", ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	cmd := platform.Request{
		ID:     "buyer",
		UserID: "admin",
		Role:   "admin",
		Data:   map[string]any{"active": false, "expected_active": true, "reason": "运营审核"},
	}
	value, err := repo.SetActive(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if value["active"] != false {
		t.Fatal("account remains active")
	}
	var session domain.Session
	db.First(&session)
	if session.RevokedAt == nil {
		t.Fatal("active login survived account suspension")
	}
	list, err := repo.AdminList(ctx, platform.Request{Data: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(list)
	if strings.Contains(string(body), "private-") || strings.Contains(string(body), "password") {
		t.Fatal("private credentials leaked to management read")
	}
	cmd.ID = "admin"
	if _, err = repo.SetActive(ctx, cmd); err == nil {
		t.Fatal("own/admin account suspension accepted")
	}
	var audits int64
	db.Model(&domain.AccountChange{}).Count(&audits)
	if audits != 1 {
		t.Fatalf("account change audit missing: %d", audits)
	}
	cmd.ID = "buyer"
	cmd.Data = map[string]any{"active": true, "expected_active": true, "reason": "过期页面提交"}
	if _, err = repo.SetActive(ctx, cmd); err == nil {
		t.Fatal("stale account status overwritten")
	}
}
