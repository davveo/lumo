package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/campaign/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ DB *gorm.DB }

func (r *Repository) Migrate(ctx context.Context) error {
	if e := platform.MigrateBusinessIDs(ctx, r.DB); e != nil {
		return e
	}
	if e := adminconfig.Migrate(ctx, r.DB); e != nil {
		return e
	}
	if err := r.DB.WithContext(ctx).AutoMigrate(&domain.Activity{}, &domain.Hold{}); err != nil {
		return err
	}
	// Version zero is exclusively the new-rule precondition. Existing rules
	// must not be mistaken for a create request after an additive migration.
	return r.DB.WithContext(ctx).Model(&domain.Activity{}).Where("version = ?", 0).Update("version", 1).Error
}
func (r *Repository) Seed(ctx context.Context) error {
	for _, a := range []domain.Activity{
		{
			ID:        "addon-coffee",
			Kind:      constants.CampaignKindAddon,
			Threshold: 100000,
			Discount:  6000,
			Quota:     50,
			Active:    true,
		},
		{ID: "flash-demo", Kind: constants.CampaignKindFlash, Quota: 20, Active: true},
		{ID: "group-demo", Kind: constants.CampaignKindGroup, Quota: 30, Active: true},
		{
			ID:          "reduction-demo",
			Kind:        constants.CampaignKindReduction,
			Name:        "满额立减",
			Description: "商品满300元减30元，每人限享一次",
			Threshold:   30000,
			Discount:    3000,
			MinQuantity: 1,
			Quota:       1000,
			Active:      true,
		},
		{
			ID:          "discount-demo",
			Kind:        constants.CampaignKindDiscount,
			Name:        "折扣特惠",
			Description: "商品享9折，每人限享一次",
			PayRateBPS:  9000,
			MinQuantity: 1,
			Quota:       1000,
			Active:      true,
		},
		{
			ID:          "quantity-discount-demo",
			Kind:        constants.CampaignKindQuantityDiscount,
			Name:        "多件优惠",
			Description: "商品满2件享8.5折，每人限享一次",
			PayRateBPS:  8500,
			MinQuantity: 2,
			Quota:       1000,
			Active:      true,
		},
		{
			ID:          "direct-demo",
			Kind:        constants.CampaignKindDirect,
			Name:        "限量直降",
			Description: "每单立减20元，商品金额须大于20元，每人限享一次",
			Discount:    2000,
			MinQuantity: 1,
			Quota:       1000,
			Active:      true,
		},
	} {
		if e := r.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&a).Error; e != nil {
			return e
		}
	}
	return nil
}
func (r *Repository) List(ctx context.Context) (spanResult0 platform.Result, spanErr0 error) {
	ctx, s := platform.Start(ctx, "repository.campaign.List")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var a []domain.Activity
	e := r.DB.WithContext(ctx).Where("active = ?", true).Order("id").Find(&a).Error
	return platform.Result{"items": a}, e
}
func (r *Repository) Evaluate(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, s := platform.Start(ctx, "repository.campaign.Evaluate")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	sub := platform.Int(c.Data, "subtotal_cents")
	quantity := platform.Int(c.Data, "quantity")
	addon := platform.Text(c.Data, "addon_sku_id")
	id := platform.Text(c.Data, "activity_id")
	if sub < 0 || quantity < 0 {
		return nil, status.Error(codes.InvalidArgument, "negative subtotal or quantity")
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
		platform.TraceError(ctx, e)

		return nil, status.Error(codes.NotFound, "activity not found")
	}
	discount, reason, policy := evaluate(a, sub, quantity)
	return platform.Result{
		"discount_cents":  discount,
		"eligible":        reason == "",
		"reason":          reason,
		"activity_id":     id,
		"kind":            a.Kind,
		"name":            a.Name,
		"threshold_cents": a.Threshold,
		"min_quantity":    a.MinQuantity,
		"pay_rate_bps":    a.PayRateBPS,
		"policy_version":  policy,
		"refund_policy":   "full_bundle_return",
	}, nil
}

func evaluate(a domain.Activity, subtotal, quantity int64) (int64, string, string) {
	policy := "v1"
	modern := a.Kind == constants.CampaignKindReduction || a.Kind == constants.CampaignKindDiscount ||
		a.Kind == constants.CampaignKindQuantityDiscount ||
		a.Kind == constants.CampaignKindDirect
	if modern {
		policy = "v2"
	}
	reject := func(reason string) (int64, string, string) { return 0, reason, policy }
	if !a.Active {
		return reject("inactive")
	}
	if a.Threshold < 0 || a.Discount < 0 || a.Held < 0 || a.Used < 0 || a.Quota < 1 {
		return reject("invalid_rule")
	}
	if subtotal < a.Threshold {
		return reject("threshold_not_met")
	}
	if modern && (a.MinQuantity < 1 || quantity < a.MinQuantity) {
		return reject("quantity_not_met")
	}
	if a.Held >= a.Quota || a.Used >= a.Quota-a.Held {
		return reject("quota_exhausted")
	}
	discount := a.Discount
	switch a.Kind {
	case constants.CampaignKindFlash, constants.CampaignKindGroup, constants.CampaignKindAddon:
		// Preserve the existing fixed-discount rules and their zero-discount
		// flash/group quota contracts.
	case constants.CampaignKindReduction, constants.CampaignKindDirect:
		if discount < 1 {
			return reject("invalid_rule")
		}
	case constants.CampaignKindDiscount, constants.CampaignKindQuantityDiscount:
		if a.PayRateBPS < 1 || a.PayRateBPS >= 10000 {
			return reject("invalid_rule")
		}
		// Split the multiplication so public preview amounts cannot overflow.
		off := int64(10000) - a.PayRateBPS
		discount = subtotal/10000*off + subtotal%10000*off/10000
	default:
		return reject("invalid_rule")
	}
	if modern && (discount < 1 || discount >= subtotal) {
		return reject("amount_too_small")
	}
	return discount, "", policy
}
func (r *Repository) Reserve(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, s := platform.Start(ctx, "repository.campaign.Reserve")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

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
			if h.State == constants.CampaignHoldStateCancelled {
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
		if er = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(
			&a,
			"id = ?",
			id,
		).Error; er != nil {
			return status.Error(codes.NotFound, "activity not found")
		}
		if !a.Active {
			return status.Error(codes.FailedPrecondition, "inactive activity")
		}
		var count int64
		if er = tx.WithContext(ctx).Model(&domain.Hold{}).Where(
			"user_id = ? AND activity_id = ? AND state IN ?",
			c.UserID,
			id,
			[]string{constants.CampaignHoldStateReserved, constants.CampaignHoldStateConfirmed},
		).Count(
			&count,
		).Error; er != nil {
			return er
		}
		if count > 0 {
			return status.Error(codes.ResourceExhausted, "one purchase per user per activity")
		}
		h = domain.Hold{
			ID:         c.ID,
			UserID:     c.UserID,
			ActivityID: id,
			Quantity:   qty,
			State:      constants.CampaignHoldStateReserved,
		}
		if er = tx.WithContext(ctx).Create(&h).Error; er != nil {
			return er
		}
		u := tx.WithContext(ctx).
			Model(&a).
			Where("quota - held - used >= ?", qty).
			Update("held", gorm.Expr("held + ?", qty))
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
func (r *Repository) Confirm(ctx context.Context, c platform.Request) (platform.Result, error) {
	return r.transition(ctx, c, constants.CampaignHoldStateConfirmed)
}
func (r *Repository) Cancel(ctx context.Context, c platform.Request) (platform.Result, error) {
	return r.transition(ctx, c, constants.CampaignHoldStateCancelled)
}
func (r *Repository) transition(ctx context.Context, c platform.Request, target string) (spanResult0 platform.Result, spanErr0 error) {
	ctx, s := platform.Start(ctx, "repository.campaign.transition."+target)
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if c.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "order ID required")
	}
	var h domain.Hold
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&h, "id = ?", c.ID).Error
		if errors.Is(er, gorm.ErrRecordNotFound) && target == constants.CampaignHoldStateCancelled {
			h = domain.Hold{ID: c.ID, UserID: c.UserID, State: constants.CampaignHoldStateCancelled}
			return tx.WithContext(ctx).Create(&h).Error
		}
		if er != nil {
			return status.Error(codes.NotFound, "activity hold not found")
		}
		if h.UserID != c.UserID {
			return status.Error(codes.PermissionDenied, "activity hold owner mismatch")
		}
		if h.State == target {
			return nil
		}
		if h.State != constants.CampaignHoldStateReserved {
			return status.Error(codes.FailedPrecondition, "invalid activity transition")
		}
		u := map[string]any{"held": gorm.Expr("held - ?", h.Quantity)}
		if target == constants.CampaignHoldStateConfirmed {
			u["used"] = gorm.Expr("used + ?", h.Quantity)
		}
		if er = tx.WithContext(ctx).Model(&domain.Activity{}).Where(
			"id = ? AND held >= ?",
			h.ActivityID,
			h.Quantity,
		).Updates(
			u,
		).Error; er != nil {
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
