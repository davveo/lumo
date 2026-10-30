package repository

import (
	"context"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) RegistrationAttempt(ctx context.Context, ip string) (spanErr0 error) {
	ctx, span := platform.Start(ctx, "iam.repository.RegistrationAttempt")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rate := domain.AuthRate{ID: authKey("register:" + ip)}
		if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rate).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&rate, "id = ?", rate.ID).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if rate.Day != now.Format("2006-01-02") {
			rate.Day = now.Format("2006-01-02")
			rate.Count = 0
		}
		if rate.Count >= 20 {
			return status.Error(codes.ResourceExhausted, "注册请求过多，请明天再试或联系客服")
		}
		rate.Count++
		rate.LastSent = now
		return tx.WithContext(ctx).Save(&rate).Error
	})
}

func (r *Repository) Register(ctx context.Context, row *domain.Account) (spanErr0 error) {
	ctx, span := platform.Start(ctx, "iam.repository.Register")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	// The unique username index also serializes concurrent registrations.
	id, err := platform.NextBusinessID(ctx, r.db, "user")
	if err != nil {
		return err
	}
	row.ID = id
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return status.Error(codes.AlreadyExists, "该账号已被注册，请更换账号或直接登录")
	}
	return nil
}
