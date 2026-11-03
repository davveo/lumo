package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/member/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func customerView(row domain.CustomerCard) platform.Result {
	out := platform.Map(row)
	tags := []string{}
	_ = json.Unmarshal([]byte(row.Tags), &tags)
	out["tags"] = tags
	return out
}
func (r *Repository) CustomerGet(ctx context.Context, id string) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "member.repository.CustomerGet")
	defer sp.End()
	row := domain.CustomerCard{UserID: id, Tags: "[]"}
	e := r.db.WithContext(ctx).Where("user_id = ?", id).Limit(1).Find(&row).Error
	return customerView(row), e
}

func (r *Repository) CustomerUpdate(
	ctx context.Context,
	row domain.CustomerCard,
	expected int64,
) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "member.repository.CustomerUpdate")
	defer sp.End()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := domain.CustomerCard{UserID: row.UserID, Tags: "[]"}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; e != nil {
			return e
		}
		var old domain.CustomerCard
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "user_id = ?", row.UserID).Error; e != nil {
			return e
		}
		if old.Version != expected {
			return status.Error(codes.Aborted, "客户资料已被其他人更新，请刷新后重试")
		}
		row.Version = expected + 1
		if e := tx.Save(&row).Error; e != nil {
			return e
		}
		raw, _ := json.Marshal(customerView(row))
		return tx.Create(
			&domain.CustomerChange{
				ID:         platform.ID("audit"),
				CustomerID: row.UserID,
				ActorID:    row.UpdatedBy,
				Detail:     string(raw),
			},
		).Error
	})
	return customerView(row), err
}
func (r *Repository) SegmentList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "member.repository.SegmentList")
	defer sp.End()
	rows, total, f, e := adminquery.Page[domain.CustomerSegment](
		ctx,
		r.db,
		c.Data,
		adminquery.Spec{Search: []string{"id", "name"}},
	)
	return adminquery.Result(rows, total, f), e
}

func (r *Repository) SegmentUpsert(
	ctx context.Context,
	row domain.CustomerSegment,
	expected int64,
) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "member.repository.SegmentUpsert")
	defer sp.End()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := domain.CustomerSegment{ID: row.ID}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; e != nil {
			return e
		}
		var old domain.CustomerSegment
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", row.ID).Error; e != nil {
			return e
		}
		if old.Version != expected {
			return status.Error(codes.Aborted, "分群规则已更新，请刷新")
		}
		row.Version = expected + 1
		row.CreatedAt = old.CreatedAt
		return tx.Save(&row).Error
	})
	return platform.Map(row), err
}

func (r *Repository) Segment(ctx context.Context, id string) (domain.CustomerSegment, error) {
	var row domain.CustomerSegment
	e := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "客户分群不存在")
	}
	return row, e
}
