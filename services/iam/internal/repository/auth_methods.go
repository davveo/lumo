package repository

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type LoginPolicy struct {
	Password     bool  `json:"password_enabled"`
	Phone        bool  `json:"phone_enabled"`
	Wechat       bool  `json:"wechat_enabled"`
	QQ           bool  `json:"qq_enabled"`
	Registration bool  `json:"allow_registration"`
	OTPTTL       int64 `json:"otp_ttl_seconds"`
	OTPCooldown  int64 `json:"otp_cooldown_seconds"`
}

func (r *Repository) LoginPolicy(ctx context.Context) (LoginPolicy, int64, error) {
	return adminconfig.Read(ctx, r.db, "login-methods", LoginPolicy{Password: true, Registration: true, OTPTTL: 300, OTPCooldown: 60})
}
func (r *Repository) SaveLoginPolicy(ctx context.Context, c platform.Request, p LoginPolicy) (platform.Result, error) {
	return adminconfig.Save(ctx, r.db, "login-methods", p, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
}
func authKey(text string) string { h := sha256.Sum256([]byte(text)); return hex.EncodeToString(h[:]) }
func (r *Repository) NewOTP(ctx context.Context, row domain.AuthChallenge, ip string, cooldown int64) error {
	ctx, span := platform.Start(ctx, "iam.repository.NewOTP")
	defer span.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		keys := []struct {
			Key string
			Max int64
		}{{"phone:" + row.Subject, 10}, {"ip:" + ip, 50}}
		for _, limit := range keys {
			rate := domain.AuthRate{ID: authKey(limit.Key)}
			if e := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rate).Error; e != nil {
				return e
			}
			if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&rate, "id = ?", rate.ID).Error; e != nil {
				return e
			}
			now := time.Now().UTC()
			day := now.Format("2006-01-02")
			if rate.Day != day {
				rate.Count = 0
				rate.Day = day
			}
			if rate.Count >= limit.Max {
				return status.Error(codes.ResourceExhausted, "今日验证码请求过多，请稍后再试")
			}
			if limit.Max == 10 && now.Sub(rate.LastSent) < time.Duration(cooldown)*time.Second {
				return status.Error(codes.ResourceExhausted, "验证码发送过于频繁，请稍后再试")
			}
			rate.Count++
			rate.LastSent = now
			if e := tx.WithContext(ctx).Save(&rate).Error; e != nil {
				return e
			}
		}
		return tx.WithContext(ctx).Create(&row).Error
	})
}
func (r *Repository) Delivered(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&domain.AuthChallenge{}).Where("id = ? AND consumed_at IS NULL", id).Update("delivered", true).Error
}
func (r *Repository) NewOAuth(ctx context.Context, row domain.AuthChallenge, ip string) error {
	ctx, sp := platform.Start(ctx, "iam.repository.NewOAuth")
	defer sp.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rate := domain.AuthRate{ID: authKey("oauth:" + ip)}
		if e := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rate).Error; e != nil {
			return e
		}
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&rate, "id = ?", rate.ID).Error; e != nil {
			return e
		}
		now := time.Now().UTC()
		day := now.Format("2006-01-02")
		if rate.Day != day {
			rate.Day = day
			rate.Count = 0
		}
		if rate.Count >= 200 {
			return status.Error(codes.ResourceExhausted, "授权请求过多，请稍后再试")
		}
		rate.Count++
		rate.LastSent = now
		if e := tx.WithContext(ctx).Save(&rate).Error; e != nil {
			return e
		}
		return tx.WithContext(ctx).Create(&row).Error
	})
}
func (r *Repository) ConsumeOAuth(ctx context.Context, id, provider, binding string) error {
	ctx, span := platform.Start(ctx, "iam.repository.ConsumeOAuth")
	defer span.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row domain.AuthChallenge
		if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; e != nil {
			return status.Error(codes.Unauthenticated, "登录授权已失效，请重新发起")
		}
		if row.Provider != provider || row.ConsumedAt != nil || !time.Now().Before(row.ExpiresAt) || subtle.ConstantTimeCompare([]byte(row.Binding), []byte(binding)) != 1 {
			return status.Error(codes.Unauthenticated, "登录授权校验失败")
		}
		now := time.Now().UTC()
		return tx.WithContext(ctx).Model(&row).Update("consumed_at", now).Error
	})
}
func identityAccount(ctx context.Context, tx *gorm.DB, provider, subject string, register bool) (domain.Account, error) {
	identity := domain.Identity{ID: authKey(provider + "\x00" + subject), Provider: provider, Subject: subject}
	if e := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&identity).Error; e != nil {
		return domain.Account{}, e
	}
	if e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&identity, "id = ?", identity.ID).Error; e != nil {
		return domain.Account{}, e
	}
	if identity.AccountID == "" {
		if !register {
			return domain.Account{}, status.Error(codes.PermissionDenied, "商城暂未开放新会员注册")
		}
		id, e := platform.NextBusinessID(ctx, tx, "user")
		if e != nil {
			return domain.Account{}, e
		}
		account := domain.Account{ID: id, Username: "member_" + id, Role: constants.RoleUser, Active: true}
		if e = tx.WithContext(ctx).Create(&account).Error; e != nil {
			return domain.Account{}, e
		}
		identity.AccountID = id
		if e = tx.WithContext(ctx).Model(&identity).Update("account_id", id).Error; e != nil {
			return domain.Account{}, e
		}
	}
	var account domain.Account
	if e := tx.WithContext(ctx).First(&account, "id = ?", identity.AccountID).Error; e != nil {
		return account, e
	}
	if !account.Active {
		return account, status.Error(codes.PermissionDenied, "账号已停用，请联系商城客服")
	}
	return account, nil
}
func (r *Repository) IdentityAccount(ctx context.Context, provider, subject string, register bool) (domain.Account, error) {
	var account domain.Account
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		account, e = identityAccount(ctx, tx, provider, subject, register)
		return e
	})
	return account, e
}
func (r *Repository) ConsumeOTP(ctx context.Context, id, phone, digest string, register bool) (domain.Account, error) {
	ctx, span := platform.Start(ctx, "iam.repository.ConsumeOTP")
	defer span.End()
	var account domain.Account
	var rejected error
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row domain.AuthChallenge
		e := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			rejected = status.Error(codes.Unauthenticated, "验证码无效或已过期")
			return nil
		}
		if e != nil {
			return e
		}
		if row.Provider != "phone" || row.Subject != phone || !row.Delivered || row.ConsumedAt != nil || !time.Now().Before(row.ExpiresAt) || row.Attempts >= 5 {
			rejected = status.Error(codes.Unauthenticated, "验证码无效或已过期")
			return nil
		}
		if subtle.ConstantTimeCompare([]byte(row.Digest), []byte(digest)) != 1 {
			rejected = status.Error(codes.Unauthenticated, "验证码不正确")
			return tx.WithContext(ctx).Model(&row).Update("attempts", gorm.Expr("attempts + 1")).Error
		}
		account, e = identityAccount(ctx, tx, "phone", phone, register)
		if e != nil {
			return e
		}
		now := time.Now().UTC()
		return tx.WithContext(ctx).Model(&row).Update("consumed_at", now).Error
	})
	if e != nil {
		return account, e
	}
	return account, rejected
}
