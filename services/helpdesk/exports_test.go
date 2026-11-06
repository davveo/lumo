package helpdesk_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk"
	"github.com/davveo/lumo/services/helpdesk/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type exportPeer struct {
	role          string
	total         int
	reads, stores int
	content       string
	cutoff        string
	fail          bool
}

func (p *exportPeer) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	switch service {
	case "iam":
		return platform.Result{"id": c.ID, "role": p.role, "active": true}, nil
	case "order":
		p.reads++
		if p.fail {
			return nil, status.Error(codes.Unavailable, "unavailable")
		}
		if p.cutoff != "" && p.cutoff != platform.Text(c.Data, "as_of") {
			return nil, fmt.Errorf("cutoff changed")
		}
		p.cutoff = platform.Text(c.Data, "as_of")
		page := int(platform.Int(c.Data, "page"))
		rows := []map[string]any{}
		for n := (page - 1) * 100; n < min(page*100, p.total); n++ {
			rows = append(
				rows,
				map[string]any{"id": fmt.Sprintf("row-%03d", n), "reason": "\t=HYPERLINK(\"x\")", "amount_cents": -12},
			)
		}
		return platform.Result{"items": rows, "total": p.total}, nil
	case "media":
		if c.Action == "download" {
			return platform.Result{"download_url": "https://private.example/signed", "expires_in": 600}, nil
		}
		p.stores++
		raw, e := base64.StdEncoding.DecodeString(platform.Text(c.Data, "file_content"))
		if e != nil {
			return nil, e
		}
		p.content = string(raw)
		return platform.Result{"file_id": "private-csv"}, nil
	}
	return nil, status.Error(codes.Unimplemented, "unexpected source")
}
func TestExportsSurvivePagesAndRecheckAccess(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "exports.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	if e = platform.MigratePlatform(ctx, db); e != nil {
		t.Fatal(e)
	}
	peer := &exportPeer{role: "admin", total: 105}
	m := helpdesk.New(platform.Dependencies{DB: db, RPC: peer})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	c := platform.Request{
		UserID:         "admin",
		Role:           "admin",
		IdempotencyKey: "export-one",
		Data:           map[string]any{"source": "order/admin_list", "filters": map[string]any{"status": "COMPLETED"}},
	}
	result, e := m.Operations["export_create"](ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	var j struct {
		Job domain.ExportJob `json:"job"`
	}
	_ = platform.Decode(result, &j)
	if _, e = m.Operations["export_create"](ctx, c); e != nil {
		t.Fatal(e)
	}
	var count int64
	db.Model(&domain.ExportJob{}).Where("lease_until IS NULL").Count(&count)
	if count != 1 {
		t.Fatal("unclaimed exports must persist a NULL lease, never a MySQL zero date", count)
	}
	db.Model(&domain.ExportJob{}).Count(&count)
	if count != 1 {
		t.Fatal("duplicate job", count)
	}
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if peer.reads != 1 {
		t.Fatal("must fetch one page per tick", peer.reads)
	}
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if peer.reads != 2 || peer.stores != 0 {
		t.Fatal("not a durable upload boundary")
	}
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if peer.stores != 1 || !strings.Contains(peer.content, "row-104") ||
		!strings.Contains(peer.content, "'\t=HYPERLINK") {
		t.Fatal("unsafe or incomplete CSV")
	}
	if peer.cutoff == "" {
		t.Fatal("cutoff missing")
	}
	db.First(&j.Job, "id = ?", j.Job.ID)
	if j.Job.Status != "READY" || j.Job.Processed != 105 {
		t.Fatal(j.Job)
	}
	var events int64
	db.Model(&platform.Outbox{}).Where("event_type = ?", "ExportReady").Count(&events)
	if events != 1 {
		t.Fatal("ready notification missing", events)
	}
	access := platform.Request{ID: j.Job.ID, UserID: "other", Role: "admin"}
	if _, e = m.Operations["export_download"](ctx, access); status.Code(e) != codes.PermissionDenied {
		t.Fatal("foreign download allowed", e)
	}
	access.UserID = "admin"
	if _, e = m.Operations["export_download"](ctx, access); e != nil {
		t.Fatal(e)
	}
	peer.role = "staff_warehouse"
	if _, e = m.Operations["export_download"](ctx, access); status.Code(e) != codes.PermissionDenied {
		t.Fatal("revoked permission ignored", e)
	}
	peer.role = "admin"
	db.Model(&domain.ExportJob{}).Where("id = ?", j.Job.ID).Update("expires_at", time.Now().Add(-time.Second))
	if _, e = m.Operations["export_download"](ctx, access); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("expired file allowed", e)
	}
}
func TestExportRetryRetainsJobAndRestartsFailedRange(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "retry.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	_ = platform.MigratePlatform(ctx, db)
	peer := &exportPeer{role: "admin", total: 105}
	m := helpdesk.New(platform.Dependencies{DB: db, RPC: peer})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	result, e := m.Operations["export_create"](
		ctx,
		platform.Request{
			UserID:         "admin",
			Role:           "admin",
			IdempotencyKey: "retry",
			Data:           map[string]any{"source": "order/admin_list"},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	var out struct {
		Job domain.ExportJob `json:"job"`
	}
	_ = platform.Decode(result, &out)
	_ = m.Tick(ctx)
	peer.total = 104
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	db.First(&out.Job, "id = ?", out.Job.ID)
	if out.Job.Status != "FAILED" {
		t.Fatal("moving range did not fail", out.Job)
	}
	request := platform.Request{ID: out.Job.ID, UserID: "admin", Role: "admin"}
	if _, e = m.Operations["export_retry"](ctx, request); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Operations["export_retry"](ctx, request); e != nil {
		t.Fatal(e)
	}
	for range 3 {
		if e = m.Tick(ctx); e != nil {
			t.Fatal(e)
		}
	}
	db.First(&out.Job, "id = ?", out.Job.ID)
	if out.Job.Status != "READY" || out.Job.Processed != 104 {
		t.Fatal(out.Job)
	}
	var count int64
	db.Model(&domain.ExportJob{}).Count(&count)
	if count != 1 {
		t.Fatal("retry created extra task")
	}
}
