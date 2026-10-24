package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/flash/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ DB *gorm.DB }

func (r *Repository) Migrate(ctx context.Context) error {
	return r.DB.WithContext(ctx).AutoMigrate(&domain.Session{}, &domain.Request{})
}
func (r *Repository) Seed(ctx context.Context) error {
	return r.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&domain.Session{ID: "flash-demo", SKUID: "sku-headphones", Name: "限时秒杀示例场次", ActivityID: "flash-demo", Active: true}).Error
}
func (r *Repository) Session(ctx context.Context, id string) (domain.Session, error) {
	ctx, s := platform.Start(ctx, "repository.flash.Session")
	defer s.End()
	var v domain.Session
	e := r.DB.WithContext(ctx).First(&v, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return v, status.Error(codes.NotFound, "flash session not found")
	}
	return v, e
}
func (r *Repository) List(ctx context.Context) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.flash.List")
	defer s.End()
	var v []domain.Session
	e := r.DB.WithContext(ctx).Where("active = ?", true).Find(&v).Error
	return platform.Result{"items": v}, e
}
func (r *Repository) Find(ctx context.Context, user, key string) (*domain.Request, error) {
	ctx, s := platform.Start(ctx, "repository.flash.Find")
	defer s.End()
	var v domain.Request
	e := r.DB.WithContext(ctx).Where("user_id = ? AND `key` = ?", user, key).First(&v).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &v, e
}
func (r *Repository) Get(ctx context.Context, id string) (domain.Request, error) {
	ctx, s := platform.Start(ctx, "repository.flash.Get")
	defer s.End()
	var v domain.Request
	e := r.DB.WithContext(ctx).First(&v, "id = ?", id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return v, status.Error(codes.NotFound, "flash request not found")
	}
	return v, e
}
func (r *Repository) Create(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.flash.Create")
	defer s.End()
	payload, e := json.Marshal(c.Data)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid flash payload")
	}
	sum := sha256.Sum256([]byte(c.UserID + "\x00" + c.IdempotencyKey))
	id := "flash-" + hex.EncodeToString(sum[:16])
	v := domain.Request{ID: id, UserID: c.UserID, Key: c.IdempotencyKey, QuoteID: platform.Text(c.Data, "quote_id"), PayloadJSON: string(payload), State: constants.FlashRequestStatusQueued}
	e = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var prior domain.Request
		er := tx.WithContext(ctx).First(&prior, "id = ?", id).Error
		if er == nil {
			if prior.PayloadJSON != v.PayloadJSON {
				return status.Error(codes.AlreadyExists, "idempotency payload differs")
			}
			v = prior
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		if er = tx.WithContext(ctx).Create(&v).Error; er != nil {
			return er
		}
		return platform.Enqueue(ctx, tx, platform.Event{ID: id + ":requested", Type: constants.EventFlashOrderRequested, AggregateID: id, UserID: c.UserID, Data: map[string]any{"request_id": id}})
	})
	return platform.Map(v), e
}
func (r *Repository) SetProcessing(ctx context.Context, id string) error {
	ctx, s := platform.Start(ctx, "repository.flash.SetProcessing")
	defer s.End()
	return r.DB.WithContext(ctx).Model(&domain.Request{}).Where("id = ? AND state = ?", id, constants.FlashRequestStatusQueued).Update("state", constants.FlashRequestStatusProcessing).Error
}
func (r *Repository) Finish(ctx context.Context, id, order, reason string) error {
	ctx, s := platform.Start(ctx, "repository.flash.Finish")
	defer s.End()
	next := constants.FlashRequestStatusSucceeded
	if reason != "" {
		next = constants.FlashRequestStatusFailed
	}
	return r.DB.WithContext(ctx).Model(&domain.Request{}).Where("id = ? AND state IN ?", id, []string{constants.FlashRequestStatusQueued, constants.FlashRequestStatusProcessing}).Updates(map[string]any{"state": next, "order_id": order, "error": reason}).Error
}
