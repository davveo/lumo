package interaction_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/interaction"
	"github.com/davveo/lumo/services/interaction/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

type reviewPeer struct {
	imageReads  int
	verifyReads int
}

func (p *reviewPeer) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	switch service {
	case "order":
		return platform.Result{
			"id":      c.ID,
			"user_id": "buyer",
			"status":  constants.OrderStatusCompleted,
			"items":   []platform.Result{{"sku_id": "coffee", "quantity": 1}},
		}, nil
	case "catalog":
		return platform.Result{"id": c.ID, "variants": []any{}}, nil
	case "media":
		if c.Role != constants.RoleSystem || c.UserID != "buyer" {
			return nil, status.Error(codes.PermissionDenied, "file owner mismatch")
		}
		if c.Action == "read_review_image" {
			p.imageReads++
			return platform.Result{"content_type": "image/jpeg", "file_content": "image-bytes"}, nil
		}
		p.verifyReads++
		var in struct {
			IDs []string `json:"ids"`
		}
		_ = platform.Decode(c.Data, &in)
		rows := []platform.Result{}
		for _, id := range in.IDs {
			if id == "foreign" {
				return nil, status.Error(codes.PermissionDenied, "file belongs to another customer")
			}
			rows = append(rows, platform.Result{"file_id": id, "content_type": "image/png", "etag": "verified"})
		}
		return platform.Result{"items": rows}, nil
	}
	return nil, status.Error(codes.Unimplemented, "unexpected review peer")
}
func reviewSetup(t *testing.T) (platform.Module, *gorm.DB, *reviewPeer) {
	t.Helper()
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "reviews.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	if e = platform.MigratePlatform(ctx, db); e != nil {
		t.Fatal(e)
	}
	peer := &reviewPeer{}
	m := interaction.New(platform.Dependencies{DB: db, RPC: peer})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	return m, db, peer
}
func TestReviewPhotosFollowModerationAndOwnerBoundaries(t *testing.T) {
	ctx := context.Background()
	m, db, peer := reviewSetup(t)
	input := platform.Request{
		UserID: "buyer",
		Role:   "user",
		Data: map[string]any{
			"kind":      "review",
			"sku_id":    "coffee",
			"order_id":  "completed-order",
			"body":      "商品实拍体验",
			"rating":    5,
			"photo_ids": []string{"foreign"},
		},
	}
	if _, e := m.Operations["create"](ctx, input); status.Code(e) != codes.PermissionDenied {
		t.Fatal("foreign attachment accepted", e)
	}
	input.Data["photo_ids"] = []string{"photo-1"}
	created, e := m.Operations["create"](ctx, input)
	if e != nil {
		t.Fatal(e)
	}
	id := platform.Text(created, "id")
	version := platform.Int(created, "version")
	image := platform.Request{ID: id, Data: map[string]any{"file_id": "photo-1"}}
	if _, e = m.Operations["review_image"](ctx, image); status.Code(e) != codes.NotFound || peer.imageReads != 0 {
		t.Fatal("unmoderated image became public", e)
	}
	image.UserID = "buyer"
	image.Role = "user"
	if _, e = m.Operations["review_image"](ctx, image); e != nil {
		t.Fatal(e)
	}
	moderate := platform.Request{
		ID:     id,
		UserID: "admin",
		Role:   "admin",
		Data:   map[string]any{"approved": true, "version": version},
	}
	published, e := m.Operations["moderate"](ctx, moderate)
	if e != nil {
		t.Fatal(e)
	}
	image.UserID = ""
	image.Role = ""
	if _, e = m.Operations["review_image"](ctx, image); e != nil {
		t.Fatal(e)
	}
	image.Data["file_id"] = "photo-unbound"
	reads := peer.imageReads
	if _, e = m.Operations["review_image"](ctx, image); status.Code(e) != codes.NotFound || peer.imageReads != reads {
		t.Fatal("arbitrary file id forwarded to private media", e)
	}
	image.Data["file_id"] = "photo-1"
	moderate.Data = map[string]any{
		"approved": false,
		"version":  platform.Int(published, "version"),
		"reason":   "图片与商品不符，请更换实拍图",
	}
	rejected, e := m.Operations["moderate"](ctx, moderate)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Operations["review_image"](ctx, image); status.Code(e) != codes.NotFound {
		t.Fatal("withdrawn image still publicly readable", e)
	}
	owned := platform.Request{ID: id, UserID: "buyer", Role: "user"}
	detail, e := m.Operations["review_get"](ctx, owned)
	if e != nil || platform.Text(detail, "moderation_reason") != "图片与商品不符，请更换实拍图" {
		t.Fatal(detail, e)
	}
	owned.Data = map[string]any{
		"version":   platform.Int(rejected, "version") - 1,
		"body":      "更换后的实拍体验",
		"rating":    4,
		"photo_ids": []string{"photo-2"},
	}
	if _, e = m.Operations["review_revise"](ctx, owned); status.Code(e) != codes.Aborted {
		t.Fatal("stale revision accepted", e)
	}
	owned.Data["version"] = platform.Int(rejected, "version")
	revised, e := m.Operations["review_revise"](ctx, owned)
	if e != nil {
		t.Fatal(e)
	}
	again, e := m.Operations["review_revise"](ctx, owned)
	if e != nil || platform.Int(again, "version") != platform.Int(revised, "version") {
		t.Fatal("revision retry was not idempotent", again, e)
	}
	moderate.Data = map[string]any{"approved": true, "version": platform.Int(revised, "version")}
	if _, e = m.Operations["moderate"](ctx, moderate); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Operations["review_image"](ctx, image); status.Code(e) != codes.NotFound {
		t.Fatal("removed photo still attached", e)
	}
	image.Data["file_id"] = "photo-2"
	if _, e = m.Operations["review_image"](ctx, image); e != nil {
		t.Fatal(e)
	}
	var audits int64
	db.Model(&domain.Record{}).Where("kind = ?", constants.InteractionKindModeration).Count(&audits)
	if audits != 4 {
		t.Fatal("moderation/revision audit mismatch", audits)
	}
}
func TestFollowupIsSeparateAndCannotExposePrivatePhotos(t *testing.T) {
	ctx := context.Background()
	m, db, _ := reviewSetup(t)
	created, e := m.Operations["create"](
		ctx,
		platform.Request{
			UserID: "buyer",
			Role:   "user",
			Data:   map[string]any{"sku_id": "coffee", "order_id": "order", "body": "首次体验", "rating": 5},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	id := platform.Text(created, "id")
	admin := platform.Request{ID: id, UserID: "admin", Role: "admin", Data: map[string]any{"approved": true}}
	if _, e = m.Operations["moderate"](ctx, admin); e != nil {
		t.Fatal(e)
	}
	own := platform.Request{
		ID:     id,
		UserID: "buyer",
		Role:   "user",
		Data:   map[string]any{"body": "使用一周后的体验", "photo_ids": []string{"followup-photo"}, "version": 0},
	}
	if _, e = m.Operations["followup_save"](ctx, own); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("followup allowed before seven days", e)
	}
	if e = db.Model(&domain.Record{}).Where("id = ?", id).Update("created_at", time.Now().Add(-8*24*time.Hour).UTC()).Error; e != nil {
		t.Fatal(e)
	}
	pending, e := m.Operations["followup_save"](ctx, own)
	if e != nil {
		t.Fatal(e)
	}
	var p struct {
		Followup domain.ReviewFollowup `json:"followup"`
	}
	_ = platform.Decode(pending, &p)
	if p.Followup.Status != constants.InteractionStatusPending {
		t.Fatal(pending)
	}
	publicImage := platform.Request{ID: id, Data: map[string]any{"file_id": "followup-photo", "scope": "followup"}}
	if _, e = m.Operations["review_image"](ctx, publicImage); status.Code(e) != codes.NotFound {
		t.Fatal("pending followup photo was public", e)
	}
	public, e := m.Operations["list"](ctx, platform.Request{Data: map[string]any{"kind": "review", "sku_id": "coffee"}})
	if e != nil {
		t.Fatal(e)
	}
	var rows struct {
		Items []platform.Result `json:"items"`
	}
	_ = platform.Decode(public, &rows)
	if len(rows.Items) != 1 || rows.Items[0]["followup"] != nil {
		t.Fatal("pending followup text leaked", public)
	}
	admin.Data = map[string]any{"approved": true, "scope": "followup", "version": p.Followup.Version}
	if _, e = m.Operations["moderate"](ctx, admin); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Operations["review_image"](ctx, publicImage); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Operations["followup_save"](ctx, own); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("published followup overwritten", e)
	}
	admin.Data = map[string]any{"approved": false, "reason": "首评撤回"}
	if _, e = m.Operations["moderate"](ctx, admin); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Operations["review_image"](ctx, publicImage); status.Code(e) != codes.NotFound {
		t.Fatal("followup survived base withdrawal", e)
	}
}
func TestRatingSummaryIncludesAllPublishedReviewsBeyondDisplayWindow(t *testing.T) {
	ctx := context.Background()
	m, db, _ := reviewSetup(t)
	rows := []domain.Record{}
	for n := 0; n < 105; n++ {
		rating := 5
		if n == 104 {
			rating = 1
		}
		data, _ := json.Marshal(map[string]any{"sku_id": "coffee", "rating": rating, "body": "真实评价"})
		rows = append(
			rows,
			domain.Record{
				ID:       fmt.Sprintf("review-%03d", n),
				ParentID: "coffee",
				Kind:     constants.InteractionKindReview,
				Status:   constants.InteractionStatusPublished,
				Payload:  string(data),
			},
		)
	}
	if e := db.Create(&rows).Error; e != nil {
		t.Fatal(e)
	}
	out, e := m.Operations["list"](
		ctx,
		platform.Request{Data: map[string]any{"sku_id": "coffee", "kind": "review", "limit": 100}},
	)
	if e != nil {
		t.Fatal(e)
	}
	var result struct {
		Items   []platform.Result `json:"items"`
		Summary struct {
			Total     int64
			Favorable int64
			Average   float64
		} `json:"summary"`
	}
	if e = platform.Decode(out, &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Items) != 100 || result.Summary.Total != 105 || result.Summary.Favorable != 104 ||
		result.Summary.Average >= 5 {
		t.Fatal("summary reflected only display page", out)
	}
}
