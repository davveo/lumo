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

func (r *Repository) CreateImport(ctx context.Context, job *domain.ImportJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}
func (r *Repository) GetImport(ctx context.Context, id, user string) (domain.ImportJob, error) {
	var job domain.ImportJob
	err := r.db.WithContext(ctx).Where("id = ? AND actor_id = ?", id, user).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = status.Error(codes.NotFound, "导入任务不存在")
	}
	return job, err
}
func (r *Repository) StartImport(ctx context.Context, id, user string) (domain.ImportJob, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job domain.ImportJob
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND actor_id = ?", id, user).First(&job).Error; e != nil {
			return status.Error(codes.NotFound, "导入任务不存在")
		}
		if job.Status != constants.CatalogImportStatusPreview {
			if job.Status == constants.CatalogImportStatusImporting ||
				job.Status == constants.CatalogImportStatusCompleted ||
				job.Status == constants.CatalogImportStatusPartialFailed {
				return nil
			}
			return status.Error(codes.FailedPrecondition, "请修正错误行后重新预览")
		}
		if !job.ExpiresAt.After(time.Now()) {
			return status.Error(codes.FailedPrecondition, "预览已过期，请重新上传")
		}
		return tx.Model(&job).Update("status", constants.CatalogImportStatusImporting).Error
	})
	if err != nil {
		return domain.ImportJob{}, err
	}
	return r.GetImport(ctx, id, user)
}
func (r *Repository) ProcessImports(ctx context.Context) error {
	var jobs []domain.ImportJob
	if e := r.db.WithContext(ctx).Where("status = ?", constants.CatalogImportStatusImporting).Order("created_at").Limit(4).Find(&jobs).Error; e != nil {
		return e
	}
	for _, candidate := range jobs {
		if e := r.processImport(ctx, candidate.ID); e != nil {
			return e
		}
	}
	return nil
}
func (r *Repository) processImport(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job domain.ImportJob
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", id).Error; e != nil {
			return e
		}
		if job.Status != constants.CatalogImportStatusImporting {
			return nil
		}
		var rows []domain.ImportRow
		if e := json.Unmarshal([]byte(job.Rows), &rows); e != nil {
			return e
		}
		end := job.Processed + 25
		if end > len(rows) {
			end = len(rows)
		}
		for i := job.Processed; i < end; i++ {
			row := &rows[i]
			if len(row.Records) == 0 || row.Records[0].Payload == "" {
				row.Error = "旧版本预览缺少商品资料，请重新上传预览"
				row.Status = constants.CatalogImportRowStatusFailed
				job.Failed++
				continue
			}
			var current domain.Record
			result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", row.ID)
			exists := result.Error == nil
			if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return result.Error
			}
			if exists != row.Exists || exists && !current.UpdatedAt.Equal(row.Version) {
				row.Error = "商品已在预览后被修改，请重新导入"
				row.Status = constants.CatalogImportRowStatusFailed
				job.Failed++
				continue
			}
			child := New(tx)
			event := platform.Event{
				ID:          job.ID + ":" + row.ID,
				Type:        "ProductChanged",
				AggregateID: row.ID,
				UserID:      job.ActorID,
				Data:        map[string]any{"product_id": row.ID, "import_id": job.ID},
			}
			if e := child.SaveProduct(ctx, row.ID, row.Records, &event); e != nil {
				if status.Code(e) == codes.AlreadyExists {
					row.Error = status.Convert(e).Message()
					row.Status = constants.CatalogImportRowStatusFailed
					job.Failed++
					continue
				}
				return e
			}
			row.Status = constants.CatalogImportRowStatusSucceeded
			job.Succeeded++
		}
		job.Processed = end
		if end == len(rows) {
			job.Status = constants.CatalogImportStatusCompleted
			if job.Failed > 0 {
				job.Status = constants.CatalogImportStatusPartialFailed
			}
		}
		raw, e := json.Marshal(rows)
		if e != nil {
			return e
		}
		job.Rows = string(raw)
		return tx.Save(&job).Error
	})
}
