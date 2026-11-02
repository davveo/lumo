package repository

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/davveo/lumo/services/iam/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func sessionPublicID(digest string) string { return authKey("session-public:" + digest) }
func (r *Repository) SecurityIdentities(ctx context.Context, user string) ([]domain.Identity, error) {
	var rows []domain.Identity
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND provider IN ?", user, []string{"phone", "email"}).
		Find(&rows).
		Error
	return rows, err
}
func (r *Repository) SecuritySessions(ctx context.Context, user, current string) ([]map[string]any, error) {
	var rows []domain.Session
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", user, time.Now()).
		Order("created_at DESC").
		Limit(100).
		Find(&rows).
		Error
	out := []map[string]any{}
	for _, row := range rows {
		out = append(
			out,
			map[string]any{
				"id":         sessionPublicID(row.Digest),
				"created_at": row.CreatedAt,
				"expires_at": row.ExpiresAt,
				"current":    row.Digest == current,
				"device":     row.Device,
				"ip":         row.ClientIP,
			},
		)
	}
	return out, err
}
func (r *Repository) RevokeSecuritySession(ctx context.Context, user, id string) error {
	var rows []domain.Session
	if err := r.db.WithContext(ctx).Where("user_id = ? AND revoked_at IS NULL", user).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if sessionPublicID(row.Digest) == id {
			return r.db.WithContext(ctx).
				Model(&domain.Session{}).
				Where("digest = ? AND user_id = ?", row.Digest, user).
				Update("revoked_at", time.Now().UTC()).
				Error
		}
	}
	return status.Error(codes.NotFound, "登录设备不存在")
}
func (r *Repository) ChangePassword(ctx context.Context, user, oldHash, next string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account domain.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ?", user).Error; err != nil {
			return err
		}
		if !account.Active || account.PasswordHash != oldHash {
			return status.Error(codes.Aborted, "账号信息已变化，请重新验证")
		}
		if err := tx.Model(&account).Update("password_hash", next).Error; err != nil {
			return err
		}
		return tx.Model(&domain.Session{}).
			Where("user_id = ? AND revoked_at IS NULL", user).
			Update("revoked_at", time.Now().UTC()).
			Error
	})
}
func (r *Repository) NewSecurityChallenge(ctx context.Context, row *domain.SecurityChallenge, ip string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		for _, scope := range []struct {
			key      string
			max      int64
			cooldown bool
		}{{"security:" + row.Purpose + ":" + row.Channel + ":" + row.Subject, 10, true}, {"security-ip:" + ip, 50, false}} {
			rate := domain.AuthRate{ID: authKey(scope.key)}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rate).Error; err != nil {
				return err
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rate, "id = ?", rate.ID).Error; err != nil {
				return err
			}
			day := now.Format("2006-01-02")
			if rate.Day != day {
				rate.Day = day
				rate.Count = 0
			}
			if rate.Count >= scope.max ||
				scope.cooldown && rate.LastSent != nil && now.Sub(*rate.LastSent) < time.Minute {
				return status.Error(codes.ResourceExhausted, "验证码请求过于频繁，请稍后重试")
			}
			rate.Count++
			rate.LastSent = &now
			if err := tx.Save(&rate).Error; err != nil {
				return err
			}
		}
		return tx.Create(row).Error
	})
}
func (r *Repository) SecurityDelivered(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&domain.SecurityChallenge{}).Where("id = ?", id).Update("delivered", true).Error
}
func (r *Repository) ConsumeSecurity(ctx context.Context, id, digest, purpose, user, nextPassword string) error {
	var rejected error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row domain.SecurityChallenge
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			rejected = status.Error(codes.Unauthenticated, "验证码无效或已过期")
			return nil
		}
		if err != nil {
			return err
		}
		if row.Purpose != purpose || purpose == "bind" && row.AccountID != user || !row.Delivered ||
			row.ConsumedAt != nil ||
			row.Attempts >= 5 ||
			!row.ExpiresAt.After(time.Now()) {
			rejected = status.Error(codes.Unauthenticated, "验证码无效或已过期")
			return nil
		}
		if subtle.ConstantTimeCompare([]byte(row.Digest), []byte(digest)) != 1 {
			rejected = status.Error(codes.Unauthenticated, "验证码不正确")
			return tx.Model(&row).Update("attempts", gorm.Expr("attempts + 1")).Error
		}
		var account domain.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ?", row.AccountID).Error; err != nil {
			return err
		}
		if !account.Active {
			return status.Error(codes.PermissionDenied, "账号不可用")
		}
		if purpose == "bind" {
			identity := domain.Identity{
				ID:        authKey(row.Channel + "\x00" + row.Subject),
				Provider:  row.Channel,
				Subject:   row.Subject,
				AccountID: account.ID,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&identity).Error; err != nil {
				return err
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&identity, "id = ?", identity.ID).Error; err != nil {
				return err
			}
			if identity.AccountID != account.ID {
				return status.Error(codes.AlreadyExists, "该联系方式已绑定其他账号")
			}
			// One verified address per channel; obsolete addresses cannot recover this account.
			if err := tx.Where("account_id = ? AND provider = ? AND id <> ?", account.ID, row.Channel, identity.ID).Delete(&domain.Identity{}).Error; err != nil {
				return err
			}
		} else {
			// Recovery cannot act on a contact which has since been unbound/reassigned.
			var count int64
			if err := tx.Model(&domain.Identity{}).Where("account_id = ? AND provider = ? AND subject = ?", account.ID, row.Channel, row.Subject).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return status.Error(codes.FailedPrecondition, "联系方式已变化，请重新验证")
			}
			if err := tx.Model(&account).Update("password_hash", nextPassword).Error; err != nil {
				return err
			}
			if err := tx.Model(&domain.Session{}).Where("user_id = ? AND revoked_at IS NULL", account.ID).Update("revoked_at", time.Now().UTC()).Error; err != nil {
				return err
			}
		}
		return tx.Model(&row).Update("consumed_at", time.Now().UTC()).Error
	})
	if err != nil {
		return err
	}
	return rejected
}

// Serialize password login against password changes so a previously read hash
// cannot issue a fresh session after the change has revoked old sessions.
func (r *Repository) CreatePasswordSession(ctx context.Context, v *domain.Session, expected string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var a domain.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, "id = ?", v.UserID).Error; err != nil {
			return err
		}
		if !a.Active || a.PasswordHash != expected {
			return status.Error(codes.Unauthenticated, "账号信息已变化，请重新登录")
		}
		return tx.Create(v).Error
	})
}
