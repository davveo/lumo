package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *Repository) Draft(ctx context.Context, id string) (domain.ProductDraft, error) {
	var d domain.ProductDraft
	e := r.db.WithContext(ctx).First(&d, "product_id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "服务端草稿不存在")
	}
	return d, e
}

func (r *Repository) SaveDraft(
	ctx context.Context,
	d domain.ProductDraft,
	expected int64,
) (domain.ProductDraft, error) {
	ctx, sp := platform.Start(ctx, "catalog.repository.SaveDraft")
	defer sp.End()
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := domain.ProductDraft{ProductID: d.ProductID}
		if e := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; e != nil {
			return e
		}
		var held domain.ProductDraft
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, "product_id = ?", d.ProductID).Error; e != nil {
			return e
		}
		if held.Version != expected {
			return status.Error(codes.Aborted, "草稿已被其他人修改，请重新载入并比较")
		}
		d.Version = held.Version + 1
		d.CreatedAt = held.CreatedAt
		d.Status = constants.ProductDraftStatusDraft
		return tx.WithContext(ctx).Save(&d).Error
	})
	return d, e
}
func (r *Repository) Drafts(ctx context.Context, page int) (platform.Result, error) {
	var n int64
	q := r.db.WithContext(ctx).Model(&domain.ProductDraft{}).Where("status <> ?", constants.ProductDraftStatusPublished)
	if e := q.Count(&n).Error; e != nil {
		return nil, e
	}
	rows := []domain.ProductDraft{}
	e := q.Order("updated_at DESC,product_id").Offset((page - 1) * 20).Limit(20).Find(&rows).Error
	return platform.Result{"items": rows, "total": n, "page": page, "page_size": 20}, e
}
func (r *Repository) Workspace(ctx context.Context, id string, page int) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "catalog.repository.Workspace")
	defer sp.End()
	versions := []domain.ProductRevision{}
	plans := []domain.Publication{}
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.ProductRevision{}).Where("product_id = ?", id)
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	if e := q.Order("version DESC").Offset((page - 1) * 20).Limit(20).Find(&versions).Error; e != nil {
		return nil, e
	}
	var planTotal int64
	pq := r.db.WithContext(ctx).Model(&domain.Publication{}).Where("product_id = ?", id)
	if e := pq.Count(&planTotal).Error; e != nil {
		return nil, e
	}
	if e := pq.Order("created_at DESC,id").Offset((page - 1) * 20).Limit(20).Find(&plans).Error; e != nil {
		return nil, e
	}
	out := platform.Result{
		"versions":       versions,
		"plans":          plans,
		"versions_total": total,
		"plans_total":    planTotal,
		"page":           page,
		"page_size":      20,
	}
	d, e := r.Draft(ctx, id)
	if status.Code(e) == codes.NotFound {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	var product, editor map[string]any
	_ = json.Unmarshal([]byte(d.DataJSON), &product)
	_ = json.Unmarshal([]byte(d.EditorJSON), &editor)
	out["draft"] = d
	out["product"] = product
	out["editor"] = editor
	return out, nil
}
func (r *Repository) Revision(ctx context.Context, id string, version int64) (domain.ProductRevision, error) {
	var v domain.ProductRevision
	e := r.db.WithContext(ctx).First(&v, "product_id = ? AND version = ?", id, version).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "历史版本不存在")
	}
	return v, e
}
func (r *Repository) PublishDraft(ctx context.Context, d domain.ProductDraft, rows []domain.Record) error {
	ctx, sp := platform.Start(ctx, "catalog.repository.PublishDraft")
	defer sp.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var held domain.ProductDraft
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, "product_id = ?", d.ProductID).Error; e != nil {
			return e
		}
		if held.Version != d.Version {
			return status.Error(codes.Aborted, "草稿已变化，请重新检查")
		}
		child := New(tx)
		event := platform.Event{
			ID:          platform.ID("evt"),
			Type:        constants.EventProductChanged,
			AggregateID: d.ProductID,
			UserID:      d.ActorID,
			Data:        map[string]any{"source": "draft_publish"},
		}
		if e := child.SaveProductChecked(ctx, d.ProductID, rows, &event, &d.BaseVersion); e != nil {
			return e
		}
		held.Status = constants.ProductDraftStatusPublished
		held.Version++
		return tx.WithContext(ctx).Save(&held).Error
	})
}
func (r *Repository) CreatePublications(ctx context.Context, plans []domain.Publication) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&plans).Error
	})
}
func (r *Repository) DuePublication(ctx context.Context) (*domain.Publication, error) {
	var p domain.Publication
	e := r.db.WithContext(ctx).
		Where("status = ? AND run_at <= ?", constants.PublicationStatusScheduled, time.Now().UTC()).
		Order("run_at,id").
		First(&p).
		Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, e
}
func (r *Repository) ExecutePublication(ctx context.Context, p domain.Publication, checkErr error) error {
	ctx, sp := platform.Start(ctx, "catalog.repository.ExecutePublication")
	defer sp.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var held domain.Publication
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, "id = ?", p.ID).Error; e != nil {
			return e
		}
		if held.Status != constants.PublicationStatusScheduled {
			return nil
		}
		var rows []domain.Record
		e := checkErr
		if e == nil {
			rows, e = domain.DecodeRecords([]byte(held.RowsJSON))
		}
		if e == nil && len(rows) == 0 {
			e = status.Error(codes.FailedPrecondition, "发布快照为空")
		}
		if e == nil && held.DependsOnID != "" {
			var parent domain.Publication
			e = tx.WithContext(ctx).First(&parent, "id = ?", held.DependsOnID).Error
			if e == nil && parent.Status != constants.PublicationStatusSucceeded {
				e = status.Error(codes.FailedPrecondition, "前置上架计划未成功")
			}
		}
		if e == nil {
			event := platform.Event{
				ID:          platform.ID("evt"),
				Type:        constants.EventProductChanged,
				AggregateID: held.ProductID,
				UserID:      held.ActorID,
				Data:        map[string]any{"publication_id": held.ID},
			}
			e = New(tx).SaveProductChecked(ctx, held.ProductID, rows, &event, &held.BaseVersion)
		}
		if e != nil {
			held.Status = constants.PublicationStatusFailed
			held.Error = "执行前检查失败（" + status.Code(e).String() + "）：商品、权限或配送规则已变化，请重新检查并创建计划"
		} else {
			held.Status = constants.PublicationStatusSucceeded
			held.Error = ""
		}
		return tx.WithContext(ctx).Save(&held).Error
	})
}
func (r *Repository) CancelPublication(ctx context.Context, id, actor string) error {
	ctx, sp := platform.Start(ctx, "catalog.repository.CancelPublication")
	defer sp.End()
	res := r.db.WithContext(ctx).
		Model(&domain.Publication{}).
		Where("id = ? AND status = ?", id, constants.PublicationStatusScheduled).
		Updates(map[string]any{"status": constants.PublicationStatusCancelled, "error": "由 " + actor + " 取消"})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return status.Error(codes.FailedPrecondition, "计划已执行或已取消")
	}
	return nil
}
