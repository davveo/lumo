package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func (r *Repository) ClaimTemplate(ctx context.Context, c platform.Request) (domain.Template, bool, error) {
	id := platform.Text(c.Data, "template_id")
	if id == "" {
		id = c.ID
	}
	var t domain.Template
	if id == "" || len(id) > 120 {
		return t, false, status.Error(codes.InvalidArgument, "template_id required")
	}
	if err := r.DB.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = status.Error(codes.NotFound, "coupon template not found")
		}
		return t, false, err
	}
	var count int64
	err := r.DB.WithContext(ctx).
		Model(&domain.Claim{}).
		Where("user_id = ? AND template_id = ?", c.UserID, id).
		Count(&count).
		Error
	return t, count > 0, err
}
