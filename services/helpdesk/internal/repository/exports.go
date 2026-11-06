package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *Repository) CreateExport(ctx context.Context, j domain.ExportJob) (domain.ExportJob, error) {
	ctx, sp := platform.Start(ctx, "helpdesk.repository.CreateExport")
	defer sp.End()
	// Bound active work per owner without relying on a browser staying open.
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing domain.ExportJob
		if e := tx.WithContext(ctx).First(&existing, "id = ?", j.ID).Error; e == nil {
			j = existing
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var n int64
		if e := tx.WithContext(ctx).Model(&domain.ExportJob{}).Where("owner_id = ? AND status IN ?", j.OwnerID, []string{constants.ExportStatusQueued, constants.ExportStatusRunning, constants.ExportStatusUploading}).Count(&n).Error; e != nil {
			return e
		}
		if n >= 5 {
			return status.Error(codes.ResourceExhausted, "最多同时运行5个导出任务")
		}
		return tx.WithContext(ctx).Create(&j).Error
	})
	return j, e
}
func (r *Repository) Export(ctx context.Context, id string) (domain.ExportJob, error) {
	var j domain.ExportJob
	e := r.db.WithContext(ctx).First(&j, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		e = status.Error(codes.NotFound, "导出任务不存在")
	}
	return j, e
}
func (r *Repository) Exports(ctx context.Context, owner string, page int) (platform.Result, error) {
	q := r.db.WithContext(ctx).Model(&domain.ExportJob{}).Where("owner_id = ?", owner)
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	items := []domain.ExportJob{}
	e := q.Order("created_at DESC,id DESC").Offset((page - 1) * 10).Limit(10).Find(&items).Error
	return platform.Result{"items": items, "total": total, "page": page, "page_size": 10}, e
}
func (r *Repository) ClaimExport(ctx context.Context) (*domain.ExportJob, error) {
	ctx, sp := platform.Start(ctx, "helpdesk.repository.ClaimExport")
	defer sp.End()
	now := time.Now().UTC()
	var j domain.ExportJob
	e := r.db.WithContext(ctx).
		Where("status IN ? AND (claim = '' OR lease_until <= ?) AND expires_at > ?", []string{constants.ExportStatusQueued, constants.ExportStatusRunning, constants.ExportStatusUploading}, now, now).
		Order("updated_at,id").
		First(&j).
		Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	token := platform.ID("lease")
	v := r.db.WithContext(ctx).
		Model(&domain.ExportJob{}).
		Where("id = ? AND status IN ? AND (claim = '' OR lease_until <= ?)", j.ID, []string{constants.ExportStatusQueued, constants.ExportStatusRunning, constants.ExportStatusUploading}, now).
		Updates(map[string]any{"claim": token, "lease_until": now.Add(2 * time.Minute)})
	if v.Error != nil || v.RowsAffected != 1 {
		return nil, v.Error
	}
	j.Claim = token
	return &j, nil
}

func (r *Repository) ExportProgress(
	ctx context.Context,
	j domain.ExportJob,
	rows []map[string]any,
	total int64,
	processErr error,
) error {
	ctx, sp := platform.Start(ctx, "helpdesk.repository.ExportProgress")
	defer sp.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var held domain.ExportJob
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, "id = ? AND claim = ?", j.ID, j.Claim).Error; e != nil {
			return e
		}
		if processErr != nil {
			held.Status = constants.ExportStatusFailed
			held.Error = processErr.Error()
			if len(held.Error) > 480 {
				held.Error = held.Error[:480]
			}
		} else {
			data, e := json.Marshal(rows)
			if e != nil {
				return e
			}
			if held.Bytes+int64(len(data)) > 2<<20 || total > 100000 {
				return status.Error(codes.ResourceExhausted, "导出超过2MiB或10万条，请缩小筛选范围")
			}
			keys := []domain.ExportRow{}
			for _, row := range rows {
				key := platform.Text(row, "id")
				if key == "" {
					key = platform.Text(row, "sku_id")
				}
				if key == "" {
					key = platform.Text(row, "payment_id") + ":" + platform.Text(row, "kind")
				}
				if key != "" && key != ":" {
					keys = append(keys, domain.ExportRow{JobID: j.ID, RowID: key})
				}
			}
			if len(keys) > 0 {
				if e = tx.WithContext(ctx).Create(&keys).Error; e != nil {
					return status.Error(codes.Aborted, "记录顺序发生变化，请重试或缩小筛选范围")
				}
			}
			chunk := domain.ExportChunk{JobID: j.ID, Page: int(held.Processed/100) + 1, RowsJSON: string(data)}
			if e = tx.WithContext(ctx).Create(&chunk).Error; e != nil {
				return e
			}
			held.Total = total
			held.Processed += int64(len(rows))
			held.Bytes += int64(len(data))
			held.Status = constants.ExportStatusRunning
			if held.Processed >= total {
				held.Status = constants.ExportStatusUploading
			}
		}
		held.Claim = ""
		held.LeaseUntil = nil
		return tx.WithContext(ctx).Save(&held).Error
	})
}
func (r *Repository) ExportRows(ctx context.Context, id string) ([]map[string]any, error) {
	chunks := []domain.ExportChunk{}
	if e := r.db.WithContext(ctx).Where("job_id = ?", id).Order("page").Find(&chunks).Error; e != nil {
		return nil, e
	}
	rows := []map[string]any{}
	for _, chunk := range chunks {
		var batch []map[string]any
		if e := json.Unmarshal([]byte(chunk.RowsJSON), &batch); e != nil {
			return nil, e
		}
		rows = append(rows, batch...)
	}
	return rows, nil
}
func (r *Repository) FinishExport(ctx context.Context, j domain.ExportJob, file string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.WithContext(ctx).
			Model(&domain.ExportJob{}).
			Where("id = ? AND claim = ?", j.ID, j.Claim).
			Updates(map[string]any{"status": constants.ExportStatusReady, "file_id": file, "claim": "", "lease_until": time.Now().UTC(), "error": ""})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return status.Error(codes.Aborted, "导出处理权已变化")
		}
		event := platform.Event{
			ID:          "export-ready-" + j.ID,
			Type:        constants.EventExportReady,
			UserID:      j.OwnerID,
			AggregateID: j.ID,
			Data:        map[string]any{"source": j.Source, "total": j.Total},
		}
		return platform.Enqueue(ctx, tx, event)
	})
}
func (r *Repository) RetryExport(ctx context.Context, j domain.ExportJob) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var held domain.ExportJob
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, "id = ?", j.ID).Error; e != nil {
			return e
		}
		if held.Status != constants.ExportStatusFailed {
			return status.Error(codes.FailedPrecondition, "仅失败任务可重试")
		}
		if !time.Now().Before(held.ExpiresAt) {
			return status.Error(codes.FailedPrecondition, "任务已到期，请重新创建")
		}
		if e := tx.WithContext(ctx).Where("job_id = ?", j.ID).Delete(&domain.ExportChunk{}).Error; e != nil {
			return e
		}
		if e := tx.WithContext(ctx).Where("job_id = ?", j.ID).Delete(&domain.ExportRow{}).Error; e != nil {
			return e
		}
		return tx.WithContext(ctx).
			Model(&held).
			Updates(map[string]any{"status": constants.ExportStatusQueued, "processed": 0, "total": -1, "bytes": 0, "claim": "", "error": ""}).
			Error
	})
}

// Keep failure conversion separate: chunk insertion errors must roll back first.
func (r *Repository) FailExport(ctx context.Context, j domain.ExportJob, e error) error {
	return r.ExportProgress(ctx, j, nil, j.Total, fmt.Errorf("%s", e.Error()))
}
