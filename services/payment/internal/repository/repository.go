package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/payment/internal/domain"
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
	ctx, s := platform.Start(ctx, "payment.repository.Migrate")
	defer s.End()
	return r.db.WithContext(ctx).AutoMigrate(&domain.Gate{}, &domain.Payment{})
}
func (r *Repository) Transaction(ctx context.Context, fn func(*Tx) error) error {
	ctx, s := platform.Start(ctx, "payment.repository.Transaction")
	defer s.End()
	return r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error { return fn(&Tx{db, ctx}) })
}
func (t *Tx) Gate(id string) (domain.Gate, error) {
	g := domain.Gate{OrderID: id}
	if err := t.db.WithContext(t.ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&g).Error; err != nil {
		return g, err
	}
	err := t.db.WithContext(t.ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&g, "order_id = ?", id).Error
	return g, err
}
func (t *Tx) Freeze(g *domain.Gate) error {
	g.Frozen = true
	g.Version++
	return t.db.WithContext(t.ctx).Save(g).Error
}
func (t *Tx) ByOrder(id string) (domain.Payment, error) {
	var p domain.Payment
	err := t.db.WithContext(t.ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, "order_id = ?", id).Error
	return p, err
}
func (t *Tx) Get(id string) (domain.Payment, error) {
	var p domain.Payment
	err := t.db.WithContext(t.ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, "id = ?", id).Error
	return p, err
}
func (t *Tx) Create(p *domain.Payment) error { return t.db.WithContext(t.ctx).Create(p).Error }
func (t *Tx) Save(p *domain.Payment) error   { return t.db.WithContext(t.ctx).Save(p).Error }
func (t *Tx) Emit(e platform.Event) error    { return platform.Enqueue(t.ctx, t.db, e) }
func (r *Repository) Get(ctx context.Context, id string) (domain.Payment, error) {
	ctx, s := platform.Start(ctx, "payment.repository.Get")
	defer s.End()
	var p domain.Payment
	err := r.db.WithContext(ctx).First(&p, "id = ?", id).Error
	return p, err
}
func (r *Repository) List(ctx context.Context, user string, all bool) ([]domain.Payment, error) {
	ctx, s := platform.Start(ctx, "payment.repository.List")
	defer s.End()
	var p []domain.Payment
	q := r.db.WithContext(ctx)
	if !all {
		q = q.Where("user_id = ?", user)
	}
	err := q.Order("created_at desc").Limit(100).Find(&p).Error
	return p, err
}
