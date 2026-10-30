package channelconfig

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCredentialsEncryptedMaskedRetainedAndVersioned(t *testing.T) {
	t.Setenv("CHANNEL_CONFIG_ENCRYPTION_KEY", strings.Repeat("19", 32))
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "channels.db"))
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := db.DB()
	defer sql.Close()
	if e = adminconfig.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	schema := Schema{ID: "qq", Name: "QQ", Fields: []Field{{Key: "app_id", Label: "应用", Required: true}, {Key: "app_secret", Label: "密钥", Secret: true, Required: true}}}
	current, e := Read(ctx, db, "iam-qq", map[string]string{"app_id": "env-app", "app_secret": "env-private-secret"})
	if e != nil {
		t.Fatal(e)
	}
	c := platform.Request{UserID: "admin", Data: map[string]any{"values": map[string]string{"app_id": "new-app", "app_secret": "saved-private-secret"}, "version": 0, "reason": "配置渠道"}}
	next, e := Merge(schema, current, c)
	if e != nil {
		t.Fatal(e)
	}
	public, e := Save(ctx, db, "iam-qq", schema, next, c)
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), "saved-private-secret") {
		t.Fatal("credential returned to browser")
	}
	var doc adminconfig.Document
	if e = db.WithContext(ctx).First(&doc, "id = ?", "iam-qq").Error; e != nil {
		t.Fatal(e)
	}
	var changes []adminconfig.Change
	db.WithContext(ctx).Find(&changes)
	audit, _ := json.Marshal(changes)
	if strings.Contains(doc.Payload, "saved-private-secret") || strings.Contains(string(audit), "saved-private-secret") {
		t.Fatal("credential stored without encryption")
	}
	current, e = Read(ctx, db, "iam-qq", nil)
	if e != nil || current.Values["app_secret"] != "saved-private-secret" {
		t.Fatal(current, e)
	}
	c.Data["values"] = map[string]string{"app_id": "changed", "app_secret": ""}
	c.Data["version"] = 1
	next, e = Merge(schema, current, c)
	if e != nil {
		t.Fatal(e)
	}
	if next.Values["app_secret"] != "saved-private-secret" {
		t.Fatal("blank field erased credential")
	}
	if _, e = Save(ctx, db, "iam-qq", schema, next, c); e != nil {
		t.Fatal(e)
	}
	if _, e = Save(ctx, db, "iam-qq", schema, next, c); status.Code(e) != codes.Aborted {
		t.Fatal("stale version accepted", e)
	}
	c.Data["clear_secrets"] = []string{"app_secret"}
	next, e = Merge(schema, current, c)
	if e != nil || next.Values["app_secret"] != "" {
		t.Fatal(next, e)
	}
	t.Setenv("CHANNEL_CONFIG_ENCRYPTION_KEY", strings.Repeat("28", 32))
	if _, e = Read(ctx, db, "iam-qq", nil); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("wrong key silently accepted", e)
	}
}
