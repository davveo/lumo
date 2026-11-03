package catalog_test

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"testing"
	"time"
)

type publicationPeer struct {
	role     string
	shipping bool
}

func (p *publicationPeer) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	if service == "iam" {
		return platform.Result{"role": p.role, "active": true}, nil
	}
	return platform.Result{"config": map[string]any{"enabled": p.shipping, "mode": "flat", "name": "标准配送"}}, nil
}
func TestProductDraftsPublishWithVersionsAndPlansNeverTouchInventory(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "publishing.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	_ = platform.MigratePlatform(ctx, db)
	peer := &publicationPeer{role: "admin", shipping: true}
	m := catalog.New(platform.Dependencies{DB: db, RPC: peer})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	call := func(action string, data map[string]any) (platform.Result, error) {
		return m.Operations[action](
			ctx,
			platform.Request{
				ID:             "draft-product",
				UserID:         "admin",
				Role:           "admin",
				IdempotencyKey: "schedule",
				Data:           data,
			},
		)
	}
	product := map[string]any{
		"name":          "草稿商品",
		"category":      "家电",
		"image":         "hero.jpg",
		"price_cents":   10000,
		"colors":        []string{"白"},
		"options":       []string{"标准", "高配"},
		"defaultOption": "标准",
		"prices":        []float64{100, 150},
	}
	_, e = call("draft_save", map[string]any{"product": product, "version": 0, "base_version": 0})
	if e != nil {
		t.Fatal(e)
	}
	var count int64
	db.Model(&domain.Record{}).Count(&count)
	if count != 0 {
		t.Fatal("draft leaked into live catalog")
	}
	if _, e = call("draft_save", map[string]any{"product": product, "version": 0}); status.Code(e) != codes.Aborted {
		t.Fatal("stale draft overwrote changes", e)
	}
	peer.shipping = false
	check, e := call("publish_check", nil)
	if e != nil || check["ok"] != false {
		t.Fatal("disabled shipping passed", check, e)
	}
	peer.shipping = true
	published, e := call("draft_publish", map[string]any{"version": 1})
	if e != nil {
		t.Fatal(e)
	}
	if platform.Int(published, "product_version") != 1 || published["active"] != true {
		t.Fatal(published)
	}
	product["name"] = "准备第二版"
	_, e = call("draft_save", map[string]any{"product": product, "version": 2, "base_version": 1})
	if e != nil {
		t.Fatal(e)
	}
	result, e := call(
		"publish_schedule",
		map[string]any{
			"mode":    "on",
			"version": 3,
			"run_at":  time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"ends_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	var plans struct {
		Plans []domain.Publication `json:"plans"`
	}
	_ = platform.Decode(result, &plans)
	if len(plans.Plans) != 2 || plans.Plans[1].DependsOnID != plans.Plans[0].ID {
		t.Fatal("unlinked end schedule")
	}
	if e = db.Model(&domain.Publication{}).Where("id = ?", plans.Plans[0].ID).Update("run_at", time.Now().Add(-time.Hour).UTC()).Error; e != nil {
		t.Fatal(e)
	}
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	current, e := call("get", nil)
	if e != nil || platform.Text(current, "name") != "准备第二版" || platform.Int(current, "product_version") != 2 {
		t.Fatal(current, e)
	}
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var revisions int64
	db.Model(&domain.ProductRevision{}).Count(&revisions)
	if revisions != 2 {
		t.Fatal("scheduled duplicate execution", revisions)
	}
	db.Model(&domain.Publication{}).
		Where("id = ?", plans.Plans[1].ID).
		Update("run_at", time.Now().Add(-time.Minute).UTC())
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	current, _ = call("get", nil)
	if current["active"] != false || platform.Int(current, "product_version") != 3 {
		t.Fatal(current)
	}
	restored, e := call("version_restore", map[string]any{"revision": 1, "product_version": 3})
	if e != nil || restored["active"] != true || platform.Text(restored, "name") != "草稿商品" ||
		platform.Int(restored, "product_version") != 4 {
		t.Fatal(restored, e)
	}
	if _, e = call("version_restore", map[string]any{"revision": 1, "product_version": 3}); status.Code(
		e,
	) != codes.Aborted {
		t.Fatal("stale restore succeeded", e)
	}
	if db.Migrator().HasTable("inventory_stocks") || db.Migrator().HasTable("order_orders") {
		t.Fatal("catalog crossed schema boundary")
	}
}
func TestPairedUnpublishCannotRunAfterFailedPublication(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "dependency.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	_ = platform.MigratePlatform(ctx, db)
	peer := &publicationPeer{role: "admin", shipping: true}
	m := catalog.New(platform.Dependencies{DB: db, RPC: peer})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	product := map[string]any{"name": "商品", "price_cents": 100, "category": "家居", "image": "hero.jpg", "active": true}
	req := platform.Request{ID: "p", UserID: "admin", Role: "admin", Data: product}
	if _, e = m.Operations["upsert"](ctx, req); e != nil {
		t.Fatal(e)
	}
	req.Data = map[string]any{"product": product, "base_version": 0, "version": 0}
	if _, e = m.Operations["draft_save"](ctx, req); e != nil {
		t.Fatal(e)
	}
	req.IdempotencyKey = "pair"
	req.Data = map[string]any{
		"mode":    "on",
		"version": 1,
		"run_at":  time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"ends_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
	}
	if _, e = m.Operations["publish_schedule"](ctx, req); e != nil {
		t.Fatal(e)
	}
	db.Model(&domain.Publication{}).Where("mode = ?", "on").Update("run_at", time.Now().Add(-2*time.Hour).UTC())
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	db.Model(&domain.Publication{}).Where("mode = ?", "off").Update("run_at", time.Now().Add(-time.Hour).UTC())
	if e = m.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	req.Data = nil
	current, e := m.Operations["get"](ctx, req)
	if e != nil || current["active"] != true {
		t.Fatal("failed on-plan disabled an unrelated version", current, e)
	}
	var failed int64
	db.Model(&domain.Publication{}).Where("status = ?", "FAILED").Count(&failed)
	if failed != 2 {
		t.Fatal("dependency failure not retained", failed)
	}
}
