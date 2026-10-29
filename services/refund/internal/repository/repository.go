package repository

import (
	"context"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/refund/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }
type Tx struct {
	db  *gorm.DB
	ctx context.Context
}

func New(db *gorm.DB) *Repository { return &Repository{db} }
func (r *Repository) Migrate(ctx context.Context) error {
	if e := platform.MigrateBusinessIDs(ctx, r.db); e != nil {
		return e
	}
	ctx, s := platform.Start(ctx, "refund.repository.Migrate")
	defer s.End()
	return r.db.WithContext(ctx).AutoMigrate(&domain.Cap{}, &domain.Refund{})
}
func (r *Repository) Transaction(ctx context.Context, fn func(*Tx) error) error {
	ctx, s := platform.Start(ctx, "refund.repository.Transaction")
	defer s.End()
	return r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error { return fn(&Tx{db, ctx}) })
}
func (t *Tx) Cap(id string, paid int64) (domain.Cap, error) {
	c := domain.Cap{PaymentID: id, Paid: paid}
	if err := t.db.WithContext(t.ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&c).Error; err != nil {
		return c, err
	}
	err := t.db.WithContext(t.ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "payment_id = ?", id).Error
	return c, err
}
func (t *Tx) SaveCap(c *domain.Cap) error { return t.db.WithContext(t.ctx).Save(c).Error }
func (t *Tx) Get(id string) (domain.Refund, error) {
	var r domain.Refund
	err := t.db.WithContext(t.ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, "id = ?", id).Error
	return r, err
}
func (t *Tx) Create(r *domain.Refund) error { return t.db.WithContext(t.ctx).Create(r).Error }
func (t *Tx) Save(r *domain.Refund) error   { return t.db.WithContext(t.ctx).Save(r).Error }
func (t *Tx) Emit(e platform.Event) error   { return platform.Enqueue(t.ctx, t.db, e) }
func (r *Repository) Get(ctx context.Context, id string) (domain.Refund, error) {
	ctx, s := platform.Start(ctx, "refund.repository.Get")
	defer s.End()
	if mapped, e := platform.LookupBusinessID(ctx, r.db, "refund", id); e != nil {
		return domain.Refund{}, e
	} else if mapped != "" {
		id = mapped
	}
	var v domain.Refund
	err := r.db.WithContext(ctx).First(&v, "id = ?", id).Error
	return v, err
}
func (r *Repository) List(ctx context.Context, user string, all bool) ([]domain.Refund, error) {
	ctx, s := platform.Start(ctx, "refund.repository.List")
	defer s.End()
	var v []domain.Refund
	q := r.db.WithContext(ctx)
	if !all {
		q = q.Where("user_id = ?", user)
	}
	err := q.Order("created_at desc").Limit(100).Find(&v).Error
	return v, err
}
