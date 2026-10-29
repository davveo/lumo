package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/adminconfig"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/logistics/internal/domain"
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
	ctx, s := platform.Start(ctx, "repository.logistics.Migrate")
	defer s.End()
	if e := adminconfig.Migrate(ctx, r.DB); e != nil {
		return e
	}
	return r.DB.WithContext(ctx).AutoMigrate(&domain.Record{})
}
func (r *Repository) Get(ctx context.Context, id string) (*domain.Record, error) {
	ctx, s := platform.Start(ctx, "repository.logistics.Get")
	defer s.End()
	var x domain.Record
	err := r.DB.WithContext(ctx).First(&x, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, status.Error(codes.NotFound, "logistics record not found")
	}
	return &x, err
}
func (r *Repository) List(ctx context.Context, user string) ([]domain.Record, error) {
	ctx, s := platform.Start(ctx, "repository.logistics.List")
	defer s.End()
	out := []domain.Record{}
	q := r.DB.WithContext(ctx)
	if user != "" {
		q = q.Where("user_id = ?", user)
	}
	err := q.Order("created_at desc").Limit(100).Find(&out).Error
	return out, err
}
func (r *Repository) Create(ctx context.Context, x *domain.Record) error {
	ctx, s := platform.Start(ctx, "repository.logistics.Create")
	defer s.End()
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(x)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var old domain.Record
			if err := tx.WithContext(ctx).First(&old, "id = ?", x.ID).Error; err != nil {
				return err
			}
			if old.UserID != x.UserID {
				return status.Error(codes.PermissionDenied, "resource ownership mismatch")
			}
			*x = old
		}
		return nil
	})
}

func (r *Repository) Transition(
	ctx context.Context,
	id string,
	allowed []string,
	next string,
	fields map[string]any,
	event *platform.Event,
) (*domain.Record, error) {
	ctx, s := platform.Start(ctx, "repository.logistics.Transition")
	defer s.End()
	var row domain.Record
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(
			&row,
			"id = ?",
			id,
		).Error; err != nil {
			return err
		}
		if row.Status == next {
			return nil
		}
		ok := false
		for _, a := range allowed {
			if row.Status == a {
				ok = true
			}
		}
		if !ok {
			return status.Error(codes.FailedPrecondition, "invalid logistics transition from "+row.Status+" to "+next)
		}
		if fields == nil {
			fields = map[string]any{}
		}
		fields["status"] = next
		fields["version"] = gorm.Expr("version + 1")
		q := tx.WithContext(ctx).
			Model(&domain.Record{}).
			Where("id = ? AND version = ?", id, row.Version).
			Updates(fields)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return status.Error(codes.Aborted, "concurrent logistics transition")
		}
		if event != nil {
			if err := platform.Enqueue(ctx, tx, *event); err != nil {
				return err
			}
		}
		return tx.WithContext(ctx).First(&row, "id = ?", id).Error
	})
	return &row, err
}
