package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/interaction/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"time"
)

func photoIDs(data map[string]any) ([]string, error) {
	var input struct {
		IDs []string `json:"photo_ids"`
	}
	if e := platform.Decode(data, &input); e != nil {
		return nil, e
	}
	if len(input.IDs) > 5 {
		return nil, bad("最多上传5张商品实拍图")
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, id := range input.IDs {
		if id == "" || len(id) > 128 || strings.TrimSpace(id) != id || seen[id] {
			return nil, bad("图片编号无效或重复")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}
func (s *Service) verifyReviewPhotos(ctx context.Context, user string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	result, e := platform.NewClients(
		s.rpc,
	).Media.VerifyFiles(
		ctx,
		platform.Request{
			UserID: user,
			Role:   constants.RoleSystem,
			Data:   map[string]any{"ids": ids, "max_size_bytes": 2 << 20, "require_etag": true},
		},
	)
	if e != nil {
		return e
	}
	var files struct {
		Items []map[string]any `json:"items"`
	}
	if e = platform.Decode(result, &files); e != nil {
		return e
	}
	if len(files.Items) != len(ids) {
		return status.Error(codes.FailedPrecondition, "图片校验结果不完整")
	}
	for _, file := range files.Items {
		typ := platform.Text(file, "content_type")
		if typ != constants.MediaContentTypeJPEG && typ != constants.MediaContentTypePNG {
			return bad("晒图仅支持JPEG或PNG")
		}
	}
	return nil
}
func followupView(row domain.ReviewFollowup, private bool) platform.Result {
	out := platform.Map(row)
	ids := []string{}
	_ = json.Unmarshal([]byte(row.PhotosJSON), &ids)
	out["photo_ids"] = ids
	if !private {
		delete(out, "moderated_by")
		delete(out, "reason")
	}
	return out
}

func (s *Service) decorateReviews(
	ctx context.Context,
	c platform.Request,
	rows []domain.Record,
) ([]platform.Result, error) {
	ids := []string{}
	for _, row := range rows {
		if row.Kind == constants.InteractionKindReview {
			ids = append(ids, row.ID)
		}
	}
	decorations, e := s.repo.ReviewDecorations(ctx, ids, c.UserID)
	if e != nil {
		return nil, e
	}
	out := make([]platform.Result, 0, len(rows))
	for _, row := range rows {
		item := view(row)
		private := row.UserID == c.UserID || c.Role == constants.RoleAdmin
		if !private {
			delete(item, "moderated_by")
			delete(item, "moderation_reason")
		}
		if row.Kind == constants.InteractionKindReview {
			d := decorations[row.ID]
			item["helpful_count"] = d.Likes
			item["liked"] = d.Liked
			available := row.CreatedAt.Add(7 * 24 * time.Hour)
			item["followup_available_at"] = available.UTC().Format(time.RFC3339)
			item["can_followup"] = row.UserID == c.UserID && row.Status == constants.InteractionStatusPublished &&
				!time.Now().Before(available) &&
				(d.Followup == nil || d.Followup.Status == constants.InteractionStatusRejected)
			if d.Followup != nil &&
				(private || row.Status == constants.InteractionStatusPublished && d.Followup.Status == constants.InteractionStatusPublished) {
				item["followup"] = followupView(*d.Followup, private)
			}
		}
		out = append(out, item)
	}
	return out, nil
}
func (s *Service) ReviewGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "interaction.usecase.ReviewGet")
	defer sp.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	row, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if row.Kind != constants.InteractionKindReview {
		return nil, bad("此记录不是购买评价")
	}
	if e = own(c, row); e != nil {
		return nil, e
	}
	items, e := s.decorateReviews(ctx, c, []domain.Record{row})
	if e != nil {
		return nil, e
	}
	return items[0], nil
}
func reviewText(data map[string]any) (string, error) {
	body := strings.TrimSpace(platform.Text(data, "body"))
	if body == "" || len([]rune(body)) > 2000 {
		return "", bad("评价内容需1至2000字")
	}
	return body, nil
}
func (s *Service) FollowupSave(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "interaction.usecase.FollowupSave")
	defer sp.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	body, e := reviewText(c.Data)
	if e != nil {
		return nil, e
	}
	ids, e := photoIDs(c.Data)
	if e != nil {
		return nil, e
	}
	base, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if e = own(c, base); e != nil {
		return nil, e
	}
	if base.UserID != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "仅原作者可以追加评价")
	}
	if e = s.verifyReviewPhotos(ctx, c.UserID, ids); e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(ids)
	_, e = s.repo.SaveFollowup(
		ctx,
		c.UserID,
		domain.ReviewFollowup{ReviewID: c.ID, Body: body, PhotosJSON: string(raw)},
		platform.Int(c.Data, "version"),
	)
	if e != nil {
		return nil, e
	}
	return s.ReviewGet(ctx, c)
}
func (s *Service) ReviewRevise(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "interaction.usecase.ReviewRevise")
	defer sp.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	body, e := reviewText(c.Data)
	if e != nil {
		return nil, e
	}
	rating := platform.Int(c.Data, "rating")
	if rating < 1 || rating > 5 {
		return nil, bad("rating must be 1..5")
	}
	ids, e := photoIDs(c.Data)
	if e != nil {
		return nil, e
	}
	row, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if row.UserID != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "仅原作者可以修改评价")
	}
	if e = s.verifyReviewPhotos(ctx, c.UserID, ids); e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(ids)
	if _, e = s.repo.ReviseReview(ctx, c.ID, c.UserID, body, rating, string(raw), platform.Int(c.Data, "version")); e != nil {
		return nil, e
	}
	return s.ReviewGet(ctx, c)
}
func (s *Service) ReviewImage(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "interaction.usecase.ReviewImage")
	defer sp.End()
	file := platform.Text(c.Data, "file_id")
	if c.ID == "" || len(c.ID) > 128 || file == "" || len(file) > 128 {
		return nil, bad("评价与图片编号必填")
	}
	row, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if row.Kind != constants.InteractionKindReview {
		return nil, status.Error(codes.NotFound, "图片不存在")
	}
	private := c.UserID == row.UserID || c.Role == constants.RoleAdmin
	scope := platform.Text(c.Data, "scope")
	var ids []string
	if scope == "followup" {
		f, e := s.repo.Followup(ctx, row.ID)
		if e != nil {
			return nil, e
		}
		if !private &&
			(row.Status != constants.InteractionStatusPublished || f.Status != constants.InteractionStatusPublished) {
			return nil, status.Error(codes.NotFound, "图片尚未公开或已撤回")
		}
		_ = json.Unmarshal([]byte(f.PhotosJSON), &ids)
	} else if scope == "" || scope == "review" {
		if !private && row.Status != constants.InteractionStatusPublished {
			return nil, status.Error(codes.NotFound, "图片尚未公开或已撤回")
		}
		ids, e = photoIDs(payload(row))
		if e != nil {
			return nil, e
		}
	} else {
		return nil, bad("无效的图片范围")
	}
	found := false
	for _, id := range ids {
		if id == file {
			found = true
		}
	}
	if !found {
		return nil, status.Error(codes.NotFound, "图片未绑定到此评价")
	}
	return platform.NewClients(
		s.rpc,
	).Media.ReadReviewImage(
		ctx,
		platform.Request{ID: file, UserID: row.UserID, Role: constants.RoleSystem},
	)
}
func (s *Service) moderateContent(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	approved, ok := c.Data["approved"].(bool)
	if !ok {
		return nil, bad("approved boolean required")
	}
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	if len([]rune(reason)) > 500 {
		return nil, bad("审核说明最多500字")
	}
	if !approved && reason == "" {
		reason = "内容未通过审核，请修改后重新提交或联系商家"
	}
	scope := platform.Text(c.Data, "scope")
	base, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if scope == "followup" {
		f, e := s.repo.Followup(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		version := f.Version
		if _, exists := c.Data["version"]; exists {
			version = platform.Int(c.Data, "version")
		}
		if approved {
			var ids []string
			_ = json.Unmarshal([]byte(f.PhotosJSON), &ids)
			if e = s.verifyReviewPhotos(ctx, base.UserID, ids); e != nil {
				return nil, e
			}
		}
		if _, e = s.repo.ModerateFollowup(ctx, c.ID, c.UserID, version, approved, reason); e != nil {
			return nil, e
		}
		return s.ReviewGet(ctx, c)
	}
	if scope != "" && scope != "review" {
		return nil, bad("不支持的审核范围")
	}
	version := base.Version
	if _, exists := c.Data["version"]; exists {
		version = platform.Int(c.Data, "version")
	}
	if base.Kind != constants.InteractionKindReview && base.Kind != constants.InteractionKindQuestion {
		return nil, bad("此记录不支持内容审核")
	}
	if approved && base.Kind == constants.InteractionKindReview {
		ids, e := photoIDs(payload(base))
		if e != nil {
			return nil, e
		}
		if e = s.verifyReviewPhotos(ctx, base.UserID, ids); e != nil {
			return nil, e
		}
	}
	row, e := s.repo.Mutate(
		ctx,
		domain.Record{ID: c.ID},
		false,
		func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			if v.Version != version {
				return nil, nil, status.Error(codes.Aborted, "内容已更新，请重新读取后审核")
			}
			previous := v.Status
			v.Status = constants.InteractionStatusRejected
			if approved {
				v.Status = constants.InteractionStatusPublished
				reason = ""
			}
			data := payload(*v)
			data["moderation_reason"] = reason
			data["moderated_by"] = c.UserID
			encoded, _ := json.Marshal(data)
			v.Payload = string(encoded)
			audit := record(
				platform.ID("moderation"),
				c.UserID,
				constants.InteractionKindModeration,
				v.Status,
				map[string]any{"scope": "review", "from": previous, "to": v.Status, "reason": reason},
			)
			audit.ParentID = v.ID
			eventType := constants.EventQAChanged
			if v.Kind == constants.InteractionKindReview {
				eventType = constants.EventReviewModerated
				if approved {
					eventType = constants.EventReviewPublished
				}
			}
			return []domain.Record{
					audit,
				}, &platform.Event{
					ID:          platform.ID("evt"),
					Type:        eventType,
					UserID:      v.UserID,
					AggregateID: v.ID,
					Data:        view(*v),
				}, nil
		},
	)
	if e != nil {
		return nil, e
	}
	if row.Kind == constants.InteractionKindReview {
		return s.ReviewGet(ctx, c)
	}
	return view(row), nil
}
