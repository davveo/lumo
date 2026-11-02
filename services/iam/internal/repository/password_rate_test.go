package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPasswordLimitsPersistAcrossRepositoriesAndExpire(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "rate.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	defer raw.Close()
	r := New(db)
	if e = r.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 5; i++ {
		if e = r.BeginPasswordLogin(ctx, "buyer", "203.0.113.1"); e != nil {
			t.Fatal(e)
		}
		if e = r.FinishPasswordLogin(ctx, "buyer", false); e != nil {
			t.Fatal(e)
		}
	}
	other := New(db)
	if e = other.BeginPasswordLogin(ctx, "buyer", "203.0.113.2"); status.Code(e) != codes.ResourceExhausted {
		t.Fatal("account backoff bypassed", e)
	}
	if e = other.BeginPasswordLogin(ctx, "another-buyer", "203.0.113.1"); e != nil {
		t.Fatal("unrelated account blocked", e)
	}
	if e = db.Model(&PasswordRate{}).Where("id = ?", passwordRateID("buyer")).Updates(map[string]any{"blocked_until": time.Now().Add(-time.Second), "window_started": time.Now().Add(-2 * time.Minute)}).Error; e != nil {
		t.Fatal(e)
	}
	if e = other.BeginPasswordLogin(ctx, "buyer", "203.0.113.1"); e != nil {
		t.Fatal("rate did not expire", e)
	}
	if e = other.FinishPasswordLogin(ctx, "buyer", true); e != nil {
		t.Fatal(e)
	}
	var row PasswordRate
	db.First(&row, "id = ?", passwordRateID("buyer"))
	if row.Failures != 0 || row.BlockedUntil != nil {
		t.Fatal("successful login did not reset failure backoff", row)
	}
}

func TestPasswordIPLimitCoversDifferentUsernames(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "ip.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	defer raw.Close()
	r := New(db)
	if e = r.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 60; i++ {
		if e = r.BeginPasswordLogin(ctx, string(rune('a'+i)), "203.0.113.1"); e != nil {
			t.Fatal(i, e)
		}
	}
	if e = r.BeginPasswordLogin(ctx, "different-account", "203.0.113.1"); status.Code(e) != codes.ResourceExhausted {
		t.Fatal("IP limit bypassed", e)
	}
	if e = r.BeginPasswordLogin(ctx, "different-account", "203.0.113.2"); e != nil {
		t.Fatal(e)
	}
}
