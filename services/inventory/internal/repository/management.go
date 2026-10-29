package repository

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
)

func (r *Repository) AdminList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "inventory.repository.AdminList")
	defer sp.End()
	var f adminquery.Filters
	if e := platform.Decode(c.Data, &f); e != nil {
		return nil, e
	}
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = 20
	}
	if f.Page < 1 || f.Page > 1000000 || f.PageSize < 1 || f.PageSize > 100 || len([]rune(f.Keyword)) > 120 {
		return nil, status.Error(codes.InvalidArgument, "invalid inventory page")
	}
	q := r.DB.WithContext(ctx).Model(&domain.Stock{})
	if f.Keyword != "" {
		q = q.Where("sku_id LIKE ? ESCAPE '!'", "%"+strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.TrimSpace(f.Keyword))+"%")
	}
	switch f.Status {
	case "low":
		q = q.Where("on_hand-reserved-allocated > 0 AND on_hand-reserved-allocated <= threshold")
	case "out":
		q = q.Where("on_hand-reserved-allocated <= 0")
	case "healthy":
		q = q.Where("on_hand-reserved-allocated > threshold")
	case "":
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid stock filter")
	}
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	rows := []domain.Stock{}
	if e := q.Order("sku_id").Limit(f.PageSize).Offset((f.Page - 1) * f.PageSize).Find(&rows).Error; e != nil {
		return nil, e
	}
	items := []platform.Result{}
	for _, v := range rows {
		row := platform.Map(v)
		row["id"] = v.SKUID
		row["available"] = v.OnHand - v.Reserved - v.Allocated
		items = append(items, row)
	}
	return adminquery.Result(items, total, f), nil
}
func (r *Repository) Threshold(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "inventory.repository.Threshold")
	defer sp.End()
	var in struct {
		Threshold int64  `json:"threshold"`
		Expected  *int64 `json:"expected_threshold"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if c.ID == "" || in.Expected == nil || in.Threshold < 0 || in.Threshold > 1000000000 {
		return nil, status.Error(codes.InvalidArgument, "SKU, threshold and expected threshold required")
	}
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	if reason == "" || len([]rune(reason)) > 240 {
		return nil, status.Error(codes.InvalidArgument, "threshold change reason required")
	}
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old domain.Stock
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "sku_id = ?", c.ID).Error; e != nil {
			return e
		}
		if old.Threshold != *in.Expected {
			return status.Error(codes.Aborted, "threshold changed; reload before saving")
		}
		if e := tx.WithContext(ctx).Model(&domain.Stock{}).Where("sku_id = ?", c.ID).Update("threshold", in.Threshold).Error; e != nil {
			return e
		}
		before, _ := json.Marshal(map[string]any{"threshold": old.Threshold})
		after, _ := json.Marshal(map[string]any{"threshold": in.Threshold})
		return tx.WithContext(ctx).Create(&adminconfig.Change{ID: platform.ID("threshold-change"), DocumentID: c.ID, ActorID: c.UserID, Reason: reason, Before: string(before), After: string(after)}).Error
	})
	if err != nil {
		return nil, err
	}

	return r.Get(ctx, c.ID)
}
func (r *Repository) Ledger(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "inventory.repository.Ledger")
	defer sp.End()
	db := r.DB.WithContext(ctx)
	if c.ID != "" {
		db = db.Where("sku_id = ?", c.ID)
	}
	rows, total, f, e := adminquery.Page[domain.Ledger](ctx, db, c.Data, adminquery.Spec{State: "kind", Search: []string{"id", "sku_id", "reference_id", "user_id"}})
	return adminquery.Result(rows, total, f), e
}
