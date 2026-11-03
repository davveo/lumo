package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strings"
	"time"
)

func (r *Repository) WorkPolicy(ctx context.Context) (commercepolicy.Workbench, error) {
	p, v, e := adminconfig.Read(ctx, r.db, "workbench-policy", commercepolicy.DefaultWorkbench())
	p.Version = v
	return p, e
}

func (r *Repository) WorkPolicySave(
	ctx context.Context,
	c platform.Request,
	p commercepolicy.Workbench,
) (platform.Result, error) {
	return adminconfig.Save(
		ctx,
		r.db,
		"workbench-policy",
		p,
		platform.Int(c.Data, "version"),
		c.UserID,
		platform.Text(c.Data, "reason"),
	)
}
func (r *Repository) Assignments(ctx context.Context, keys []string) (map[string]domain.WorkAssignment, error) {
	rows := []domain.WorkAssignment{}
	if len(keys) > 0 {
		if e := r.db.WithContext(ctx).Where("id IN ?", keys).Find(&rows).Error; e != nil {
			return nil, e
		}
	}
	out := map[string]domain.WorkAssignment{}
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}
func (r *Repository) WorkChanges(ctx context.Context, key string) ([]domain.WorkChange, error) {
	rows := []domain.WorkChange{}
	e := r.db.WithContext(ctx).Where("work_id = ?", key).Order("created_at DESC,id DESC").Limit(100).Find(&rows).Error
	return rows, e
}

type AssignmentInput struct {
	Service         string `json:"service"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
	OrderID         string `json:"-"`
}

func (r *Repository) Assign(
	ctx context.Context,
	c platform.Request,
	items []AssignmentInput,
	target, name string,
	manager bool,
) ([]platform.Result, error) {
	sort.Slice(
		items,
		func(i, j int) bool { return items[i].Service+":"+items[i].ID < items[j].Service+":"+items[j].ID },
	)
	out := []platform.Result{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, item := range items {
			key := item.Service + ":" + item.ID
			result := platform.Result{"service": item.Service, "id": item.ID, "ok": false}
			var old domain.WorkAssignment
			e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", key).Error
			missing := errors.Is(e, gorm.ErrRecordNotFound)
			if e != nil && !missing {
				return e
			}
			if !missing && old.RequestKey == assignmentRequestKey(c) {
				if old.AssigneeID == target {
					result["ok"] = true
					result["version"] = old.Version
					result["unchanged"] = true
				} else {
					result["error"] = "同一请求编号不能更改分派目标"
				}
				out = append(out, result)
				continue
			}
			if old.Version != item.ExpectedVersion {
				result["error"] = "负责人已变化，请刷新后重试"
				out = append(out, result)
				continue
			}
			if !manager && old.AssigneeID != "" && old.AssigneeID != c.UserID {
				result["error"] = "只能领取未分派任务或调整自己的任务"
				out = append(out, result)
				continue
			}
			next := domain.WorkAssignment{
				ID:           key,
				Service:      item.Service,
				ResourceID:   item.ID,
				OrderID:      item.OrderID,
				AssigneeID:   target,
				AssigneeName: name,
				ActorID:      c.UserID,
				Version:      old.Version + 1,
				RequestKey:   assignmentRequestKey(c),
				UpdatedAt:    time.Now().UTC(),
			}
			if missing {
				q := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&next)
				if q.Error != nil {
					return q.Error
				}
				if q.RowsAffected != 1 {
					result["error"] = "任务正在被其他人分派，请刷新"
					out = append(out, result)
					continue
				}
			} else {
				if e = tx.Save(&next).Error; e != nil {
					return e
				}
			}
			if e = tx.Create(&domain.WorkChange{
				ID:      platform.ID("work-change"),
				WorkID:  key,
				ActorID: c.UserID,
				Before:  old.AssigneeID,
				After:   target,
				Reason:  platform.Text(c.Data, "reason"),
			}).Error; e != nil {
				return e
			}
			result["ok"] = true
			result["version"] = next.Version
			out = append(out, result)
		}
		return nil
	})
	return out, err
}
func (r *Repository) Views(ctx context.Context, user string) ([]domain.WorkView, error) {
	rows := []domain.WorkView{}
	e := r.db.WithContext(ctx).Where("user_id = ?", user).Order("updated_at DESC,id DESC").Limit(20).Find(&rows).Error
	return rows, e
}
func (r *Repository) SaveView(ctx context.Context, c platform.Request, name, filters string) (platform.Result, error) {
	hash := sha256.Sum256([]byte(c.UserID + "\x00" + strings.TrimSpace(name)))
	id := hex.EncodeToString(hash[:])
	version := platform.Int(c.Data, "version")
	var row domain.WorkView
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		guard := domain.WorkViewGuard{UserID: c.UserID}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&guard).Error; e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&guard, "user_id = ?", c.UserID).Error; e != nil {
			return e
		}
		e := tx.First(&row, "id = ?", id).Error
		missing := errors.Is(e, gorm.ErrRecordNotFound)
		if e != nil && !missing {
			return e
		}
		if row.Version != version {
			return status.Error(codes.Aborted, "视图已变化，请刷新")
		}
		if c.Data["delete"] == true {
			if missing {
				return nil
			}
			return tx.Delete(&row).Error
		}
		if missing {
			var count int64
			if e = tx.Model(&domain.WorkView{}).Where("user_id = ?", c.UserID).Count(&count).Error; e != nil {
				return e
			}
			if count >= 20 {
				return status.Error(codes.ResourceExhausted, "每人最多保存20个工作视图")
			}
		}
		row = domain.WorkView{
			ID:          id,
			UserID:      c.UserID,
			Name:        strings.TrimSpace(name),
			FiltersJSON: filters,
			Version:     version + 1,
			UpdatedAt:   time.Now().UTC(),
		}
		return tx.Save(&row).Error
	})
	return platform.Map(row), e
}
func assignmentRequestKey(c platform.Request) string {
	h := sha256.Sum256([]byte(c.UserID + "\x00" + c.IdempotencyKey))
	return hex.EncodeToString(h[:])
}
