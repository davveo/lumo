package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/interaction/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type ReviewDecoration struct {
	Followup *domain.ReviewFollowup
	Likes    int64
	Liked    bool
}

func (r *Repository) ReviewDecorations(
	ctx context.Context,
	ids []string,
	user string,
) (map[string]ReviewDecoration, error) {
	ctx, sp := platform.Start(ctx, "interaction.repository.ReviewDecorations")
	defer sp.End()
	out := map[string]ReviewDecoration{}
	if len(ids) == 0 {
		return out, nil
	}
	var likes []struct {
		ParentID string
		Total    int64
	}
	if e := r.db.WithContext(ctx).Model(&domain.Record{}).Where("parent_id IN ? AND kind = ?", ids, constants.InteractionKindLike).Select("parent_id,COUNT(*) AS total").Group("parent_id").Scan(&likes).Error; e != nil {
		return nil, e
	}
	for _, v := range likes {
		out[v.ParentID] = ReviewDecoration{Likes: v.Total}
	}
	if user != "" {
		var liked []domain.Record
		if e := r.db.WithContext(ctx).Where("parent_id IN ? AND kind = ? AND user_id = ?", ids, constants.InteractionKindLike, user).Find(&liked).Error; e != nil {
			return nil, e
		}
		for _, v := range liked {
			d := out[v.ParentID]
			d.Liked = true
			out[v.ParentID] = d
		}
	}
	rows := []domain.ReviewFollowup{}
	if e := r.db.WithContext(ctx).Where("review_id IN ?", ids).Find(&rows).Error; e != nil {
		return nil, e
	}
	for i := range rows {
		d := out[rows[i].ReviewID]
		d.Followup = &rows[i]
		out[rows[i].ReviewID] = d
	}
	return out, nil
}
func (r *Repository) Followup(ctx context.Context, id string) (domain.ReviewFollowup, error) {
	var v domain.ReviewFollowup
	e := r.db.WithContext(ctx).First(&v, "review_id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "追评不存在")
	}
	return v, e
}

func (r *Repository) SaveFollowup(
	ctx context.Context,
	user string,
	row domain.ReviewFollowup,
	expected int64,
) (domain.ReviewFollowup, error) {
	ctx, sp := platform.Start(ctx, "interaction.repository.SaveFollowup")
	defer sp.End()
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var base domain.Record
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&base, "id = ?", row.ReviewID).Error; e != nil {
			return e
		}
		if base.UserID != user || base.Kind != constants.InteractionKindReview {
			return status.Error(codes.PermissionDenied, "只能追评本人购买评价")
		}
		if base.Status != constants.InteractionStatusPublished ||
			time.Now().Before(base.CreatedAt.Add(7*24*time.Hour)) {
			return status.Error(codes.FailedPrecondition, "初评发布且提交满7天后可追加使用体验")
		}
		var held domain.ReviewFollowup
		e := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&held, "review_id = ?", row.ReviewID).
			Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if e == nil && held.Status == constants.InteractionStatusPending && held.Version == expected+1 &&
			held.Body == row.Body &&
			held.PhotosJSON == row.PhotosJSON {
			row = held
			return nil
		}
		if e == nil && held.Status == constants.InteractionStatusPublished {
			return status.Error(codes.FailedPrecondition, "追评已发布，暂不支持再次追加")
		}
		if held.Version != expected {
			return status.Error(codes.Aborted, "追评版本已变化，请重新载入")
		}
		row.Version = expected + 1
		row.Status = constants.InteractionStatusPending
		row.CreatedAt = held.CreatedAt
		return tx.WithContext(ctx).Save(&row).Error
	})
	return row, e
}

func (r *Repository) ModerateFollowup(
	ctx context.Context,
	id, actor string,
	expected int64,
	approved bool,
	reason string,
) (domain.ReviewFollowup, error) {
	ctx, sp := platform.Start(ctx, "interaction.repository.ModerateFollowup")
	defer sp.End()
	var out domain.ReviewFollowup
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var base domain.Record
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&base, "id = ?", id).Error; e != nil {
			return e
		}
		if base.Kind != constants.InteractionKindReview {
			return status.Error(codes.InvalidArgument, "此内容不支持追评审核")
		}
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "review_id = ?", id).Error; e != nil {
			return e
		}
		if out.Version != expected {
			return status.Error(codes.Aborted, "追评已被更新，请重新审核")
		}
		previous := out.Status
		out.Status = constants.InteractionStatusRejected
		if approved {
			if base.Status != constants.InteractionStatusPublished {
				return status.Error(codes.FailedPrecondition, "请先通过首评，再审核追评")
			}
			out.Status = constants.InteractionStatusPublished
			reason = ""
		}
		out.Reason = reason
		out.ModeratedBy = actor
		out.Version++
		if e := tx.WithContext(ctx).Save(&out).Error; e != nil {
			return e
		}
		return reviewAudit(ctx, tx, id, actor, "followup", previous, out.Status, reason)
	})
	return out, e
}
func reviewAudit(ctx context.Context, tx *gorm.DB, id, actor, scope, previous, next, reason string) error {
	data, _ := json.Marshal(map[string]any{"scope": scope, "from": previous, "to": next, "reason": reason})
	row := domain.Record{
		ID:       platform.ID("moderation"),
		ParentID: id,
		UserID:   actor,
		Kind:     constants.InteractionKindModeration,
		Status:   next,
		Payload:  string(data),
	}
	return tx.WithContext(ctx).Create(&row).Error
}

func (r *Repository) ReviseReview(
	ctx context.Context,
	id, user, body string,
	rating int64,
	photos string,
	expected int64,
) (domain.Record, error) {
	ctx, sp := platform.Start(ctx, "interaction.repository.ReviseReview")
	defer sp.End()
	var row domain.Record
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; e != nil {
			return e
		}
		if row.UserID != user || row.Kind != constants.InteractionKindReview {
			return status.Error(codes.PermissionDenied, "只能修改本人的购买评价")
		}
		var data map[string]any
		_ = json.Unmarshal([]byte(row.Payload), &data)
		var oldPhotos []string
		raw, _ := json.Marshal(data["photo_ids"])
		_ = json.Unmarshal(raw, &oldPhotos)
		canonical, _ := json.Marshal(oldPhotos)
		if row.Status == constants.InteractionStatusPending && row.Version == expected+1 &&
			platform.Text(data, "body") == body &&
			platform.Int(data, "rating") == rating &&
			string(canonical) == photos {
			return nil
		}
		if row.Status != constants.InteractionStatusRejected {
			return status.Error(codes.FailedPrecondition, "仅未通过审核的初评可以修改")
		}
		if row.Version != expected {
			return status.Error(codes.Aborted, "评价已更新，请重新读取")
		}
		var ids []string
		_ = json.Unmarshal([]byte(photos), &ids)
		data["body"] = body
		data["rating"] = rating
		data["photo_ids"] = ids
		data["moderation_reason"] = ""
		data["moderated_by"] = ""
		encoded, _ := json.Marshal(data)
		row.Payload = string(encoded)
		row.Status = constants.InteractionStatusPending
		row.Version++
		if e := tx.WithContext(ctx).Save(&row).Error; e != nil {
			return e
		}
		return reviewAudit(
			ctx,
			tx,
			id,
			user,
			"review_revision",
			constants.InteractionStatusRejected,
			constants.InteractionStatusPending,
			"",
		)
	})
	return row, e
}
