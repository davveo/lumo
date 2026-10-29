package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
	"unicode/utf8"
)

func (r *Repository) SetActive(ctx context.Context, c platform.Request) (platform.Result, error) {
	var input struct {
		Active   *bool  `json:"active"`
		Expected *bool  `json:"expected_active"`
		Reason   string `json:"reason"`
	}
	if err := platform.Decode(c.Data, &input); err != nil {
		return nil, err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if c.ID == "" || c.ID == c.UserID || input.Active == nil || input.Expected == nil || input.Reason == "" ||
		utf8.RuneCountInString(input.Reason) > 240 {
		return nil, status.Error(
			codes.InvalidArgument,
			"target account, expected status and reason required; own account cannot be changed",
		)
	}
	var out domain.Account
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", c.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return status.Error(codes.NotFound, "account not found")
		}
		if err != nil {
			return err
		}
		if out.Role == constants.RoleAdmin {
			return status.Error(codes.FailedPrecondition, "administrator availability cannot be changed here")
		}
		if out.Active == *input.Active {
			return nil
		}
		if out.Active != *input.Expected {
			return status.Error(codes.Aborted, "account status changed; reload before editing")
		}
		if err = tx.Model(&out).Update("active", *input.Active).Error; err != nil {
			return err
		}
		out.Active = *input.Active
		if !out.Active {
			now := time.Now()
			if err = tx.Model(&domain.Session{}).Where("user_id = ? AND revoked_at IS NULL", out.ID).Update("revoked_at", now).Error; err != nil {
				return err
			}
		}
		return tx.Create(
			&domain.AccountChange{
				ID:      platform.ID("account-change"),
				UserID:  out.ID,
				ActorID: c.UserID,
				Active:  out.Active,
				Reason:  input.Reason,
			},
		).Error
	})
	return platform.Map(out), err
}
