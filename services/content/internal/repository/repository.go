package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/content/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }
func (r *Repository) Migrate(ctx context.Context) (spanErr0 error) {
	if e := platform.MigrateBusinessIDs(ctx, r.db); e != nil {
		return e
	}
	if e := adminconfig.Migrate(ctx, r.db); e != nil {
		return e
	}
	ctx, s := platform.Start(ctx, "content.repository.Migrate")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).
		AutoMigrate(
			&domain.Record{},
			&domain.PopupCampaign{},
			&domain.PopupSettings{},
			&domain.PopupVisitor{},
			&domain.PopupSubject{},
			&domain.PopupCounter{},
			&domain.PopupDelivery{},
		)
}
func (r *Repository) Get(ctx context.Context, id string) (spanResult0 domain.Record, spanErr0 error) {
	ctx, s := platform.Start(ctx, "content.repository.Get")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var v domain.Record
	err := r.db.WithContext(ctx).First(&v, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return v, status.Error(codes.NotFound, "record not found")
	}
	return v, err
}
func (r *Repository) List(ctx context.Context, owner, kind, st string, limit, offset int) (spanResult0 []domain.Record, spanErr0 error) {
	ctx, s := platform.Start(ctx, "content.repository.List")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	q := r.db.WithContext(ctx).Model(&domain.Record{})
	if owner != "" {
		q = q.Where("user_id = ?", owner)
	}
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if st != "" {
		if st == constants.ContentStatusPublished {
			now := time.Now().UTC()
			q = q.Where("(status = ? OR (status = ? AND publish_at <= ?)) AND (publish_at IS NULL OR publish_at <= ?) AND (unpublish_at IS NULL OR unpublish_at > ?)", st, constants.ContentStatusScheduled, now, now, now)
		} else {
			q = q.Where("status = ?", st)
		}
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var vs []domain.Record
	err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&vs).Error
	return vs, err
}
func (r *Repository) Save(ctx context.Context, v *domain.Record, event *platform.Event) (spanErr0 error) {
	ctx, s := platform.Start(ctx, "content.repository.Save")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Upsert mutable columns while preserving the original creation time.
		// A newly assembled aggregate must not write a zero datetime on update.
		if err := tx.WithContext(ctx).Clauses(
			clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				DoUpdates: clause.Set(append(clause.AssignmentColumns([]string{"user_id", "parent_id", "kind", "status", "payload", "updated_at"}), clause.Assignment{Column: clause.Column{Name: "version"}, Value: gorm.Expr("version + 1")})),
			},
		).Create(
			v,
		).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).First(v, "id = ?", v.ID).Error; err != nil {
			return err
		}
		if event != nil {
			return platform.Enqueue(ctx, tx, *event)
		}
		return nil
	})
}
func (r *Repository) InsertOnce(ctx context.Context, v *domain.Record, event *platform.Event) (spanResult0 bool, spanErr0 error) {
	ctx, s := platform.Start(ctx, "content.repository.InsertOnce")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	inserted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(v)
		if res.Error != nil {
			return res.Error
		}
		inserted = res.RowsAffected == 1
		if inserted && event != nil {
			return platform.Enqueue(ctx, tx, *event)
		}
		return nil
	})
	return inserted, err
}

// Mutate serializes a document transition in its local transaction. Extra child
// records and the Outbox event commit atomically with the parent state.
func (r *Repository) Mutate(
	ctx context.Context,
	seed domain.Record,
	create bool,
	fn func(*domain.Record) ([]domain.Record, *platform.Event, error),
) (spanResult0 domain.Record, spanErr0 error) {
	ctx, s := platform.Start(ctx, "content.repository.Mutate")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	var got domain.Record
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if create {
			if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
				return err
			}
		}
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(
			&got,
			"id = ?",
			seed.ID,
		).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return status.Error(codes.NotFound, "record not found")
			}
			return err
		}
		extra, event, err := fn(&got)
		if err != nil {
			return err
		}
		if err = tx.WithContext(ctx).Save(&got).Error; err != nil {
			return err
		}
		for i := range extra {
			if err = tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(
				&extra[i],
			).Error; err != nil {
				return err
			}
		}
		if event != nil {
			return platform.Enqueue(ctx, tx, *event)
		}
		return nil
	})
	return got, err
}
func (r *Repository) Delete(ctx context.Context, id, owner string) (spanErr0 error) {
	ctx, s := platform.Start(ctx, "content.repository.Delete")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, owner).Delete(&domain.Record{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return status.Error(codes.NotFound, "record not found")
	}
	return nil
}
func (r *Repository) DeleteKind(ctx context.Context, owner, kind string) (spanErr0 error) {
	ctx, s := platform.Start(ctx, "content.repository.DeleteKind")
	defer s.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	return r.db.WithContext(ctx).Where("user_id = ? AND kind = ?", owner, kind).Delete(&domain.Record{}).Error
}
