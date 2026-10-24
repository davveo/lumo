package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Repository struct{ DB *gorm.DB }

func (r *Repository) Migrate(ctx context.Context) error {
	return r.DB.WithContext(ctx).AutoMigrate(&domain.Coupon{}, &domain.Hold{}, &domain.Ledger{}, &domain.Template{}, &domain.Claim{})
}
func (r *Repository) Seed(ctx context.Context) error {
	if e := r.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&domain.Coupon{ID: "coupon-demo-100", UserID: "demo", Amount: 10000, Minimum: 100000, State: constants.CouponStatusAvailable, ExpiresAt: time.Now().AddDate(1, 0, 0)}).Error; e != nil {
		return e
	}
	now := time.Now()
	open, upcoming := now.Add(-24*time.Hour), now.Add(24*time.Hour)
	templates := []domain.Template{
		{ID: "welcome-100", Name: "新人满1000减100", Amount: 10000, Minimum: 100000, ValidDays: 30, Remaining: 1000, Active: true, ExpiresAt: now.AddDate(1, 0, 0)},
		{ID: "everyday-10", Name: "日常好物券", Amount: 1000, Minimum: 10000, ValidDays: 7, Remaining: 2000, Active: true, ExpiresAt: now.AddDate(1, 0, 0)},
		{ID: "weekend-25", Name: "限时生活礼券", Amount: 2500, Minimum: 20000, ValidDays: 7, Remaining: 500, Active: true, StartsAt: &open, ExpiresAt: now.AddDate(0, 0, 7)},
		{ID: "upcoming-50", Name: "明日惊喜券", Amount: 5000, Minimum: 50000, ValidDays: 7, Remaining: 300, Active: true, StartsAt: &upcoming, ExpiresAt: now.AddDate(0, 0, 7)},
		{ID: "points-30", Name: "积分专享券", Amount: 3000, Minimum: 30000, ValidDays: 14, Remaining: 500, Active: true, MinPoints: 5000, ExpiresAt: now.AddDate(1, 0, 0)},
		{ID: "member-80", Name: "成长会员礼券", Amount: 8000, Minimum: 60000, ValidDays: 30, Remaining: 300, Active: true, MinGrowth: 1000, ExpiresAt: now.AddDate(1, 0, 0)},
	}
	return r.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&templates).Error
}
func (r *Repository) Templates(ctx context.Context, c platform.Command) (platform.Result, error) {
	return r.templates(ctx, c, 100)
}

// TemplateSnapshot batches read-only centre metadata; ordinary template paging
// keeps its smaller public page size for previews and existing consumers.
func (r *Repository) TemplateSnapshot(ctx context.Context, c platform.Command) (platform.Result, error) {
	return r.templates(ctx, c, 5000)
}

func (r *Repository) templates(ctx context.Context, c platform.Command, maxSize int64) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.coupon.Templates")
	defer s.End()
	page, size := platform.Int(c.Data, "page"), platform.Int(c.Data, "page_size")
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 100
	}
	if page < 1 || page > 1000000 || size < 1 || size > maxSize {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("page must be positive and page_size must be between 1 and %d", maxSize))
	}
	var total int64
	q := r.DB.WithContext(ctx).Model(&domain.Template{}).Where("active = ?", true)
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	var templates []domain.Template
	if e := q.Order("id").Offset(int((page - 1) * size)).Limit(int(size)).Find(&templates).Error; e != nil {
		return nil, e
	}
	items, now, err := r.templateItems(ctx, c, templates)
	if err != nil {
		return nil, err
	}
	return platform.Result{"items": items, "total": total, "page": page, "page_size": size, "has_more": page*size < total, "server_time": now}, nil
}

func (r *Repository) templateItems(ctx context.Context, c platform.Command, templates []domain.Template) ([]platform.Result, time.Time, error) {
	var claims []domain.Claim
	if c.UserID != "" && len(templates) > 0 {
		ids := make([]string, 0, len(templates))
		for _, v := range templates {
			ids = append(ids, v.ID)
		}
		if e := r.DB.WithContext(ctx).Where("user_id = ? AND template_id IN ?", c.UserID, ids).Find(&claims).Error; e != nil {
			return nil, time.Time{}, e
		}
	}
	claimed := map[string]string{}
	for _, v := range claims {
		claimed[v.TemplateID] = v.CouponID
	}
	now := time.Now()
	items := make([]platform.Result, 0, len(templates))
	for _, v := range templates {
		m := platform.Map(v)
		m["claimed"] = claimed[v.ID] != ""
		state, reason := constants.CouponClaimStatusAvailable, "可立即领取"
		switch {
		case !v.Active:
			state, reason = constants.CouponClaimStatusUnavailable, "优惠券已停用"
		case claimed[v.ID] != "":
			m["coupon_id"] = claimed[v.ID]
			state, reason = constants.CouponClaimStatusClaimed, "已领取，每人限领一张"
		case !v.ExpiresAt.After(now):
			state, reason = constants.CouponClaimStatusEnded, "领取已结束"
		case v.StartsAt != nil && v.StartsAt.After(now):
			state, reason = constants.CouponClaimStatusUpcoming, "尚未到领取时间"
		case v.Remaining < 1:
			state, reason = constants.CouponClaimStatusExhausted, "优惠券已领完"
		case !validTemplate(v):
			state, reason = constants.CouponClaimStatusUnavailable, "优惠券暂不可领取"
		case c.UserID == "":
			state, reason = constants.CouponClaimStatusLoginRequired, "登录后领取"
		}
		m["claim_status"], m["claim_reason"], m["claimable"] = state, reason, state == constants.CouponClaimStatusAvailable
		items = append(items, m)
	}
	return items, now, nil
}
func validTemplate(v domain.Template) bool {
	return v.ValidDays >= 1 && v.ValidDays <= 3660 && v.Amount > 0 && v.Minimum >= 0 && v.MinPoints >= 0 && v.MinGrowth >= 0 && (v.StartsAt == nil || v.StartsAt.Before(v.ExpiresAt))
}
func (r *Repository) Claim(ctx context.Context, c platform.Command, eligible func(context.Context, domain.Template) error) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.coupon.Claim")
	defer s.End()
	id := platform.Text(c.Data, "template_id")
	if id == "" {
		id = c.ID
	}
	if id == "" || len(id) > 120 {
		return nil, status.Error(codes.InvalidArgument, "template_id required")
	}
	var out domain.Coupon
	reused := false
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var template domain.Template
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&template, "id = ?", id).Error
		if errors.Is(er, gorm.ErrRecordNotFound) {
			return status.Error(codes.NotFound, "coupon template not found")
		}
		if er != nil {
			return er
		}
		var old domain.Claim
		er = tx.WithContext(ctx).Where("user_id = ? AND template_id = ?", c.UserID, id).First(&old).Error
		if er == nil {
			reused = true
			return tx.WithContext(ctx).First(&out, "id = ?", old.CouponID).Error
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		now := time.Now()
		if !template.Active || !template.ExpiresAt.After(now) {
			return status.Error(codes.FailedPrecondition, "优惠券已停用或领取已结束")
		}
		if !validTemplate(template) {
			return status.Error(codes.FailedPrecondition, "优惠券配置暂不可用")
		}
		if template.StartsAt != nil && template.StartsAt.After(now) {
			return status.Error(codes.FailedPrecondition, "尚未到领取时间")
		}
		if template.Remaining < 1 {
			return status.Error(codes.ResourceExhausted, "优惠券已领完")
		}
		if eligible != nil {
			if er = eligible(ctx, template); er != nil {
				return er
			}
		}
		// Recheck the claim deadline after cross-service eligibility reads.
		now = time.Now()
		if !template.ExpiresAt.After(now) {
			return status.Error(codes.FailedPrecondition, "领取已结束")
		}
		sum := sha256.Sum256([]byte(c.UserID + "\x00" + id))
		claimID := "claim-" + hex.EncodeToString(sum[:])
		expires := now.AddDate(0, 0, int(template.ValidDays))
		out = domain.Coupon{ID: claimID, TemplateID: id, UserID: c.UserID, Amount: template.Amount, Minimum: template.Minimum, State: constants.CouponStatusAvailable, ExpiresAt: expires}
		if er = tx.WithContext(ctx).Create(&domain.Claim{ID: claimID, UserID: c.UserID, TemplateID: id, CouponID: claimID}).Error; er != nil {
			return er
		}
		if er = tx.WithContext(ctx).Create(&out).Error; er != nil {
			return er
		}
		u := tx.WithContext(ctx).Model(&domain.Template{}).Where("id = ? AND remaining > 0 AND active = ?", id, true).Update("remaining", gorm.Expr("remaining - 1"))
		if u.Error != nil {
			return u.Error
		}
		if u.RowsAffected != 1 {
			return status.Error(codes.ResourceExhausted, "优惠券已领完")
		}
		return nil
	})
	m := platform.Map(out)
	m["already_claimed"] = reused
	return m, e
}
func (r *Repository) List(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.coupon.List")
	defer s.End()
	// Resolve names together with owned coupons so wallet labels do not depend
	// on a public template page or whether a template is still enabled.
	v := []struct {
		domain.Coupon
		TemplateName string `json:"template_name"`
	}{}
	e := r.DB.WithContext(ctx).Model(&domain.Coupon{}).
		Select("coupons.*, COALESCE(NULLIF(templates.name, ''), '品质生活优惠券') AS template_name").
		Joins("LEFT JOIN templates ON templates.id = coupons.template_id").
		Where("coupons.user_id = ?", c.UserID).Order("coupons.id").Scan(&v).Error
	return platform.Result{"items": v}, e
}
func (r *Repository) Reserve(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.coupon.Reserve")
	defer s.End()
	id := platform.Text(c.Data, "coupon_id")
	sub := platform.Int(c.Data, "subtotal_cents")
	if c.ID == "" || id == "" || sub < 1 {
		return nil, status.Error(codes.InvalidArgument, "order, coupon and subtotal required")
	}
	var out domain.Hold
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", c.ID).Error
		if er == nil {
			if out.UserID != c.UserID {
				return status.Error(codes.PermissionDenied, "hold owner mismatch")
			}
			if out.State == constants.CouponHoldStateCancelled {
				return status.Error(codes.FailedPrecondition, "hold cancelled")
			}
			if out.CouponID != id {
				return status.Error(codes.AlreadyExists, "hold payload differs")
			}
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		var cp domain.Coupon
		if er = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&cp, "id = ?", id).Error; er != nil {
			return status.Error(codes.NotFound, "coupon not found")
		}
		if cp.UserID != c.UserID {
			return status.Error(codes.PermissionDenied, "coupon owner mismatch")
		}
		if cp.State != constants.CouponStatusAvailable || time.Now().After(cp.ExpiresAt) || sub < cp.Minimum {
			return status.Error(codes.FailedPrecondition, "coupon unavailable or threshold unmet")
		}
		out = domain.Hold{ID: c.ID, UserID: c.UserID, CouponID: id, Amount: cp.Amount, State: constants.CouponHoldStateReserved}
		if er = tx.WithContext(ctx).Create(&out).Error; er != nil {
			return er
		}
		u := tx.WithContext(ctx).Model(&cp).Where("state = ?", constants.CouponStatusAvailable).Update("state", constants.CouponStatusHeld)
		if u.Error != nil {
			return u.Error
		}
		if u.RowsAffected != 1 {
			return status.Error(codes.Aborted, "coupon already occupied")
		}
		return nil
	})
	return platform.Map(out), e
}
func (r *Repository) Transition(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.coupon."+c.Action)
	defer s.End()
	if c.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "order ID required")
	}
	var out domain.Hold
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", c.ID).Error
		if errors.Is(er, gorm.ErrRecordNotFound) && c.Action == constants.ActionCancel {
			out = domain.Hold{ID: c.ID, UserID: c.UserID, State: constants.CouponHoldStateCancelled}
			return tx.WithContext(ctx).Create(&out).Error
		}
		if er != nil {
			if !errors.Is(er, gorm.ErrRecordNotFound) {
				return er
			}
			return status.Error(codes.NotFound, "coupon hold not found")
		}
		if out.UserID != c.UserID {
			return status.Error(codes.PermissionDenied, "hold owner mismatch")
		}
		target := constants.CouponHoldStateConfirmed
		if c.Action == constants.ActionCancel {
			target = constants.CouponHoldStateCancelled
		}
		if out.State == target {
			return nil
		}
		if out.State != constants.CouponHoldStateReserved {
			return status.Error(codes.FailedPrecondition, "invalid coupon transition")
		}
		cpState := constants.CouponStatusUsed
		if target == constants.CouponHoldStateCancelled {
			var cp domain.Coupon
			if er = tx.WithContext(ctx).First(&cp, "id = ?", out.CouponID).Error; er != nil {
				return er
			}
			cpState = constants.CouponStatusAvailable
			if time.Now().After(cp.ExpiresAt) {
				cpState = constants.CouponStatusExpired
			}
		}
		if er = tx.WithContext(ctx).Model(&domain.Coupon{}).Where("id = ?", out.CouponID).Update("state", cpState).Error; er != nil {
			return er
		}
		if er = tx.WithContext(ctx).Model(&out).Update("state", target).Error; er != nil {
			return er
		}
		out.State = target
		return tx.WithContext(ctx).Create(&domain.Ledger{ID: c.ID + ":" + target, OrderID: c.ID, CouponID: out.CouponID, Kind: target}).Error
	})
	return platform.Map(out), e
}
func (r *Repository) Refund(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.coupon.Refund")
	defer s.End()
	order := platform.Text(c.Data, "order_id")
	if c.ID == "" || order == "" {
		return nil, status.Error(codes.InvalidArgument, "refund and order required")
	}
	payable := platform.Int(c.Data, "payable_cents")
	total := platform.Int(c.Data, "refunded_total_cents")
	if payable <= 0 || total < payable {
		return platform.Result{"status": constants.CouponRefundStatusNotEligible, "policy": constants.CouponRefundPolicyFullRefundOnly}, nil
	}
	result := platform.Result{"status": constants.CouponRefundStatusReturned}
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var hold domain.Hold
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&hold, "id = ?", order).Error
		if errors.Is(er, gorm.ErrRecordNotFound) {
			result["status"] = constants.CouponRefundStatusNoCoupon
			return nil
		}
		if er != nil {
			return er
		}
		if hold.UserID != c.UserID {
			return status.Error(codes.PermissionDenied, "hold owner mismatch")
		}
		if hold.State == constants.CouponHoldStateRefunded {
			return nil
		}
		if hold.State != constants.CouponHoldStateConfirmed {
			return status.Error(codes.FailedPrecondition, "coupon not consumed")
		}
		var cp domain.Coupon
		if er = tx.WithContext(ctx).First(&cp, "id = ?", hold.CouponID).Error; er != nil {
			return er
		}
		next := constants.CouponStatusAvailable
		if time.Now().After(cp.ExpiresAt) {
			next = constants.CouponStatusExpired
			result["status"] = constants.CouponRefundStatusExpiredNoReturn
		}
		if er = tx.WithContext(ctx).Model(&cp).Update("state", next).Error; er != nil {
			return er
		}
		if er = tx.WithContext(ctx).Model(&hold).Update("state", constants.CouponHoldStateRefunded).Error; er != nil {
			return er
		}
		return tx.WithContext(ctx).Create(&domain.Ledger{ID: "refund:" + c.ID, OrderID: order, CouponID: hold.CouponID, Kind: next}).Error
	})
	return result, e
}
