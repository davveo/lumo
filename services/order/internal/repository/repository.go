package repository

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"
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
	ctx, s := platform.Start(ctx, "order.repository.Migrate")
	defer s.End()
	return r.db.WithContext(ctx).AutoMigrate(&domain.Order{}, &domain.RefundReceipt{})
}
func (r *Repository) Transaction(ctx context.Context, fn func(*Tx) error) error {
	ctx, s := platform.Start(ctx, "order.repository.Transaction")
	defer s.End()
	return r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error { return fn(&Tx{db, ctx}) })
}
func (t *Tx) Get(id string) (domain.Order, error) {
	var o domain.Order
	err := t.db.WithContext(t.ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&o, "id = ?", id).Error
	return o, err
}
func (t *Tx) Create(o *domain.Order) error { return t.db.WithContext(t.ctx).Create(o).Error }
func (t *Tx) Save(o *domain.Order) error   { o.Version++; return t.db.WithContext(t.ctx).Save(o).Error }
func (t *Tx) Emit(e platform.Event) error  { return platform.Enqueue(t.ctx, t.db, e) }
func (t *Tx) Receipt(id string) (domain.RefundReceipt, error) {
	var r domain.RefundReceipt
	err := t.db.WithContext(t.ctx).First(&r, "id = ?", id).Error
	return r, err
}
func (t *Tx) AddReceipt(r domain.RefundReceipt) error {
	return t.db.WithContext(t.ctx).Create(&r).Error
}
func (r *Repository) Get(ctx context.Context, id string) (domain.Order, error) {
	ctx, s := platform.Start(ctx, "order.repository.Get")
	defer s.End()
	var o domain.Order
	err := r.db.WithContext(ctx).First(&o, "id = ?", id).Error
	return o, err
}
func (r *Repository) List(ctx context.Context, user string, all bool) ([]domain.Order, error) {
	ctx, s := platform.Start(ctx, "order.repository.List")
	defer s.End()
	var rows []domain.Order
	q := r.db.WithContext(ctx)
	if !all {
		q = q.Where("user_id = ?", user)
	}
	err := q.Order("created_at desc").Limit(100).Find(&rows).Error
	return rows, err
}
func (r *Repository) Expired(ctx context.Context) ([]domain.Order, error) {
	ctx, s := platform.Start(ctx, "order.repository.Expired")
	defer s.End()
	var rows []domain.Order
	err := r.db.WithContext(ctx).Where("status IN ? AND expires_at < CURRENT_TIMESTAMP", []string{"PENDING_PAYMENT", "CLOSING"}).Limit(30).Find(&rows).Error
	return rows, err
}
