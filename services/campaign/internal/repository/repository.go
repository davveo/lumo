package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/campaign/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ DB *gorm.DB }

func (r *Repository) Migrate(ctx context.Context) error {
	return r.DB.WithContext(ctx).AutoMigrate(&domain.Activity{}, &domain.Hold{})
}
func (r *Repository) Seed(ctx context.Context) error {
	for _, a := range []domain.Activity{{ID: "addon-coffee", Kind: "addon", Threshold: 100000, Discount: 6000, Quota: 50, Active: true}, {ID: "flash-demo", Kind: "flash", Quota: 20, Active: true}, {ID: "group-demo", Kind: "group", Quota: 30, Active: true}} {
		if e := r.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&a).Error; e != nil {
			return e
		}
	}
	return nil
}
func (r *Repository) List(ctx context.Context) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.campaign.List")
	defer s.End()
	var a []domain.Activity
	e := r.DB.WithContext(ctx).Where("active = ?", true).Order("id").Find(&a).Error
	return platform.Result{"items": a}, e
}
func (r *Repository) Evaluate(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.campaign.Evaluate")
	defer s.End()
	sub := platform.Int(c.Data, "subtotal_cents")
	addon := platform.Text(c.Data, "addon_sku_id")
	id := platform.Text(c.Data, "activity_id")
	if sub < 0 {
		return nil, status.Error(codes.InvalidArgument, "negative subtotal")
	}
	if addon != "" {
		if addon != "sku-coffee" && addon != "coffee" {
			return nil, status.Error(codes.InvalidArgument, "unsupported add-on SKU")
		}
		id = "addon-coffee"
	}
	if id == "" {
		return platform.Result{"discount_cents": int64(0), "eligible": true, "policy_version": "v1"}, nil
	}
	var a domain.Activity
	if e := r.DB.WithContext(ctx).First(&a, "id = ?", id).Error; e != nil {
		return nil, status.Error(codes.NotFound, "activity not found")
	}
	eligible := a.Active && sub >= a.Threshold && a.Held+a.Used < a.Quota
	discount := int64(0)
	if eligible {
		discount = a.Discount
	}
	return platform.Result{"discount_cents": discount, "eligible": eligible, "activity_id": id, "threshold_cents": a.Threshold, "policy_version": "v1", "refund_policy": "full_bundle_return"}, nil
}
func (r *Repository) Reserve(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.campaign.Reserve")
	defer s.End()
	id := platform.Text(c.Data, "activity_id")
	qty := platform.Int(c.Data, "quantity")
	if qty == 0 {
		qty = 1
	}
	if c.ID == "" || id == "" || qty < 1 || qty > 10 {
		return nil, status.Error(codes.InvalidArgument, "order, activity, quantity required")
	}
	var h domain.Hold
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&h, "id = ?", c.ID).Error
		if er == nil {
			if h.UserID != c.UserID {
				return status.Error(codes.PermissionDenied, "activity hold owner mismatch")
			}
			if h.State == "CANCELLED" {
				return status.Error(codes.FailedPrecondition, "activity hold cancelled")
			}
			if h.ActivityID != id || h.Quantity != qty {
				return status.Error(codes.AlreadyExists, "activity request differs")
			}
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		var a domain.Activity
		if er = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, "id = ?", id).Error; er != nil {
			return status.Error(codes.NotFound, "activity not found")
		}
		if !a.Active {
			return status.Error(codes.FailedPrecondition, "inactive activity")
		}
		var count int64
		if er = tx.WithContext(ctx).Model(&domain.Hold{}).Where("user_id = ? AND activity_id = ? AND state IN ?", c.UserID, id, []string{"RESERVED", "CONFIRMED"}).Count(&count).Error; er != nil {
			return er
		}
		if count > 0 {
			return status.Error(codes.ResourceExhausted, "one purchase per user per activity")
		}
		h = domain.Hold{ID: c.ID, UserID: c.UserID, ActivityID: id, Quantity: qty, State: "RESERVED"}
		if er = tx.WithContext(ctx).Create(&h).Error; er != nil {
			return er
		}
		u := tx.WithContext(ctx).Model(&a).Where("quota - held - used >= ?", qty).Update("held", gorm.Expr("held + ?", qty))
		if u.Error != nil {
			return u.Error
		}
		if u.RowsAffected != 1 {
			return status.Error(codes.ResourceExhausted, "activity quota exhausted")
		}
		return nil
	})
	return platform.Map(h), e
}
func (r *Repository) Transition(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.campaign."+c.Action)
	defer s.End()
	if c.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "order ID required")
	}
	var h domain.Hold
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&h, "id = ?", c.ID).Error
		if errors.Is(er, gorm.ErrRecordNotFound) && c.Action == "cancel" {
			h = domain.Hold{ID: c.ID, UserID: c.UserID, State: "CANCELLED"}
			return tx.WithContext(ctx).Create(&h).Error
		}
		if er != nil {
			return status.Error(codes.NotFound, "activity hold not found")
		}
		if h.UserID != c.UserID {
			return status.Error(codes.PermissionDenied, "activity hold owner mismatch")
		}
		target := "CONFIRMED"
		if c.Action == "cancel" {
			target = "CANCELLED"
		}
		if h.State == target {
			return nil
		}
		if h.State != "RESERVED" {
			return status.Error(codes.FailedPrecondition, "invalid activity transition")
		}
		u := map[string]any{"held": gorm.Expr("held - ?", h.Quantity)}
		if target == "CONFIRMED" {
			u["used"] = gorm.Expr("used + ?", h.Quantity)
		}
		if er = tx.WithContext(ctx).Model(&domain.Activity{}).Where("id = ? AND held >= ?", h.ActivityID, h.Quantity).Updates(u).Error; er != nil {
			return er
		}
		if er = tx.WithContext(ctx).Model(&h).Update("state", target).Error; er != nil {
			return er
		}
		h.State = target
		return nil
	})
	return platform.Map(h), e
}
