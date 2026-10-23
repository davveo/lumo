package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }
func (r *Repository) Migrate(ctx context.Context) error {
	ctx, s := platform.Start(ctx, "iam.repository.Migrate")
	defer s.End()
	return r.db.WithContext(ctx).AutoMigrate(&domain.Account{}, &domain.Session{})
}
func (r *Repository) ByUsername(ctx context.Context, name string) (domain.Account, error) {
	ctx, s := platform.Start(ctx, "iam.repository.ByUsername")
	defer s.End()
	var a domain.Account
	err := r.db.WithContext(ctx).First(&a, "username = ?", name).Error
	return a, err
}
func (r *Repository) ByID(ctx context.Context, id string) (domain.Account, error) {
	ctx, s := platform.Start(ctx, "iam.repository.ByID")
	defer s.End()
	var a domain.Account
	err := r.db.WithContext(ctx).First(&a, "id = ?", id).Error
	return a, err
}
func (r *Repository) Seed(ctx context.Context, a *domain.Account) error {
	ctx, s := platform.Start(ctx, "iam.repository.Seed")
	defer s.End()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(a).Error
}
func (r *Repository) CreateSession(ctx context.Context, v *domain.Session) error {
	ctx, s := platform.Start(ctx, "iam.repository.CreateSession")
	defer s.End()
	return r.db.WithContext(ctx).Create(v).Error
}
func (r *Repository) Session(ctx context.Context, digest string) (domain.Session, error) {
	ctx, s := platform.Start(ctx, "iam.repository.Session")
	defer s.End()
	var v domain.Session
	err := r.db.WithContext(ctx).First(&v, "digest = ?", digest).Error
	return v, err
}
func (r *Repository) Revoke(ctx context.Context, digest, user string) error {
	ctx, s := platform.Start(ctx, "iam.repository.Revoke")
	defer s.End()
	now := time.Now()
	q := r.db.WithContext(ctx).Model(&domain.Session{}).Where("revoked_at IS NULL")
	if digest != "" {
		q = q.Where("digest = ?", digest)
	} else {
		q = q.Where("user_id = ?", user)
	}
	return q.Update("revoked_at", now).Error
}
func (r *Repository) Rotate(ctx context.Context, oldDigest string, next *domain.Session) error {
	ctx, s := platform.Start(ctx, "iam.repository.Rotate")
	defer s.End()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old domain.Session
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "digest = ?", oldDigest).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return status.Error(codes.Unauthenticated, "invalid session")
			}
			return err
		}
		if old.RevokedAt != nil || !old.ExpiresAt.After(time.Now()) {
			return status.Error(codes.Unauthenticated, "session revoked or expired")
		}
		now := time.Now()
		if err := tx.WithContext(ctx).Model(&old).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Create(next).Error
	})
}
