package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/reconciliation/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *Repository) Start(ctx context.Context, row *domain.Run) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "reconciliation.repository.Start")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if result.Error != nil {
		return nil, result.Error
	}
	var saved domain.Run
	if e := r.db.WithContext(ctx).First(&saved, "id = ?", row.ID).Error; e != nil {
		return nil, e
	}
	if saved.From != row.From || saved.To != row.To || saved.ActorID != row.ActorID || saved.Reason != row.Reason {
		return nil, status.Error(codes.AlreadyExists, "reconciliation key reused with different period")
	}
	return platform.Map(saved), nil
}
func (r *Repository) AdminList(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "reconciliation.repository.AdminList")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	rows, total, f, e := adminquery.Page[domain.Run](ctx, r.db, c.Data, adminquery.Spec{State: "status", Search: []string{"id", "actor_id", "error", "reason"}})
	return adminquery.Result(rows, total, f), e
}
func (r *Repository) Get(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "reconciliation.repository.Get")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var row domain.Run
	e := r.db.WithContext(ctx).First(&row, "id = ?", c.ID).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, status.Error(codes.NotFound, "reconciliation report not found")
	}
	if e != nil {
		return nil, e
	}
	rows, total, f, e := adminquery.Page[domain.Difference](ctx, r.db.WithContext(ctx).Where("run_id = ?", c.ID), c.Data, adminquery.Spec{State: "kind", Search: []string{"payment_id", "order_id"}})
	if e != nil {
		return nil, e
	}
	notes := []domain.Followup{}
	if e = r.db.WithContext(ctx).Where("run_id = ?", c.ID).Order("created_at DESC,id DESC").Limit(50).Find(&notes).Error; e != nil {
		return nil, e
	}
	if row.Details != "" && total == 0 {
		var legacy struct {
			Differences []domain.Difference `json:"differences"`
			Checked     int64               `json:"checked_payments"`
		}
		if json.Unmarshal([]byte(row.Details), &legacy) == nil {
			row.Checked = legacy.Checked
			row.Total = legacy.Checked
			total = int64(len(legacy.Differences))
			row.DifferenceCount = total
			first := (f.Page - 1) * f.PageSize
			if first < len(legacy.Differences) {
				last := first + f.PageSize
				if last > len(legacy.Differences) {
					last = len(legacy.Differences)
				}
				rows = legacy.Differences[first:last]
			}
		}
	}
	out := platform.Map(row)
	out["differences"] = adminquery.Result(rows, total, f)
	out["notes"] = notes
	return out, nil
}
func (r *Repository) Note(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "reconciliation.repository.Note")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var count int64
	if e := r.db.WithContext(ctx).Model(&domain.Run{}).Where("id = ?", c.ID).Count(&count).Error; e != nil {
		return nil, e
	}
	if count == 0 {
		return nil, status.Error(codes.NotFound, "report not found")
	}
	hash := sha256.Sum256([]byte(c.UserID + "\x00" + c.IdempotencyKey))
	row := domain.Followup{ID: fmt.Sprintf("recon-note-%x", hash[:16]), RunID: c.ID, ActorID: c.UserID, Body: platform.Text(c.Data, "body")}
	if e := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; e != nil {
		return nil, e
	}
	var saved domain.Followup
	if e := r.db.WithContext(ctx).First(&saved, "id = ?", row.ID).Error; e != nil {
		return nil, e
	}
	if saved.RunID != row.RunID || saved.Body != row.Body {
		return nil, status.Error(codes.AlreadyExists, "note key reused with different content")
	}
	return platform.Map(saved), nil
}
func (r *Repository) Claim(ctx context.Context) (spanResult0 *domain.Run, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "reconciliation.repository.Claim")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var row domain.Run
	now := time.Now().UTC()
	e := r.db.WithContext(ctx).Where("status IN ? AND (claim = ? OR lease_until <= ?)", []string{constants.ReconciliationStatusQueued, constants.ReconciliationStatusRunning}, "", now).Order("created_at,id").First(&row).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	token := platform.ID("recon-claim")
	v := r.db.WithContext(ctx).Model(&domain.Run{}).Where("id = ? AND (claim = ? OR lease_until <= ?)", row.ID, "", now).Updates(map[string]any{"claim": token, "lease_until": now.Add(2 * time.Minute), "status": constants.ReconciliationStatusRunning})
	if v.Error != nil {
		return nil, v.Error
	}
	if v.RowsAffected == 0 {
		return nil, nil
	}
	row.Claim = token
	row.Status = constants.ReconciliationStatusRunning
	return &row, nil
}
func (r *Repository) Progress(ctx context.Context, row domain.Run, diffs []domain.Difference, processed, total int64, done bool, failure error) (spanErr0 error) {
	ctx, sp := platform.Start(ctx, "reconciliation.repository.Progress")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var held domain.Run
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, "id = ? AND claim = ?", row.ID, row.Claim).Error; e != nil {
			return e
		}
		inserted := int64(0)
		for _, diff := range diffs {
			v := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&diff)
			if v.Error != nil {
				return v.Error
			}
			inserted += v.RowsAffected
		}
		state := constants.ReconciliationStatusRunning
		if done {
			state = constants.ReconciliationStatusMatched
			if held.DifferenceCount+inserted > 0 {
				state = constants.ReconciliationStatusDifferences
			}
		}
		message := ""
		if failure != nil {
			state = constants.ReconciliationStatusFailed
			message = status.Code(failure).String() + ": " + status.Convert(failure).Message()
			if len(message) > 255 {
				message = message[:255]
			}
		}
		return tx.WithContext(ctx).Model(&domain.Run{}).Where("id = ? AND claim = ?", row.ID, row.Claim).Updates(map[string]any{"offset": held.Offset + processed, "checked": held.Checked + processed, "total": total, "difference_count": held.DifferenceCount + inserted, "status": state, "error": message, "claim": "", "lease_until": time.Now().UTC()}).Error
	})
}

func (r *Repository) Resume(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "reconciliation.repository.Resume")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	hash := sha256.Sum256([]byte(c.UserID + ":" + c.IdempotencyKey))
	id := fmt.Sprintf("resume-%x", hash[:16])
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var prior domain.Followup
		if e := tx.WithContext(ctx).Where("id = ?", id).Find(&prior).Error; e != nil {
			return e
		}
		if prior.ID != "" {
			if prior.RunID != c.ID || prior.Body != platform.Text(c.Data, "reason") {
				return status.Error(codes.AlreadyExists, "resume key reused")
			}
			return nil
		}
		result := tx.WithContext(ctx).Model(&domain.Run{}).Where("id = ? AND status = ?", c.ID, constants.ReconciliationStatusFailed).Updates(map[string]any{"status": constants.ReconciliationStatusQueued, "error": "", "claim": "", "lease_until": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return status.Error(codes.FailedPrecondition, "only failed reconciliation jobs can resume")
		}
		return tx.WithContext(ctx).Create(&domain.Followup{ID: id, RunID: c.ID, ActorID: c.UserID, Body: platform.Text(c.Data, "reason")}).Error
	})
	if e != nil {
		return nil, e
	}
	return r.Get(ctx, c)
}
