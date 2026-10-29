// Package adminconfig stores versioned configuration and audit records in the
// owning service's schema. It never chooses a database or accepts SQL from a client.
package adminconfig

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Document struct {
	ID        string    `gorm:"primaryKey;size:120" json:"id"`
	Version   int64     `json:"version"`
	Payload   string    `gorm:"type:longtext" json:"-"`
	ActorID   string    `gorm:"size:128" json:"actor_id"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Change struct {
	ID         string    `gorm:"primaryKey;size:160" json:"id"`
	DocumentID string    `gorm:"size:120;index" json:"document_id"`
	Version    int64     `json:"version"`
	ActorID    string    `gorm:"size:128" json:"actor_id"`
	Reason     string    `gorm:"size:240" json:"reason"`
	Before     string    `gorm:"type:longtext" json:"-"`
	After      string    `gorm:"type:longtext" json:"-"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

func Migrate(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).AutoMigrate(&Document{}, &Change{})
}
func Read[T any](ctx context.Context, db *gorm.DB, id string, defaults T) (T, int64, error) {
	ctx, span := platform.Start(ctx, "adminconfig.repository.Read")
	defer span.End()
	var row Document
	err := db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return defaults, 0, nil
	}
	if err != nil {
		return defaults, 0, err
	}
	var result T
	if err = json.Unmarshal([]byte(row.Payload), &result); err != nil {
		return defaults, 0, status.Error(codes.Internal, "invalid persisted configuration")
	}
	return result, row.Version, nil
}
func Save[T any](ctx context.Context, db *gorm.DB, id string, value T, expected int64, actor, reason string) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "adminconfig.repository.Save")
	defer span.End()
	if id == "" || len(id) > 120 || expected < 0 || expected == math.MaxInt64 || actor == "" || strings.TrimSpace(reason) == "" || len([]rune(reason)) > 240 {
		return nil, status.Error(codes.InvalidArgument, "configuration id, expected version and reason required")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old Document
		err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", id).Error
		missing := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !missing {
			return err
		}
		if old.Version != expected {
			return status.Error(codes.Aborted, "configuration changed; reload before saving")
		}
		next := Document{ID: id, Version: expected + 1, Payload: string(raw), ActorID: actor, UpdatedAt: time.Now().UTC()}
		if missing {
			r := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&next)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return status.Error(codes.Aborted, "configuration changed; reload before saving")
			}
		} else {
			r := tx.WithContext(ctx).Model(&Document{}).Where("id = ? AND version = ?", id, expected).Updates(map[string]any{"version": next.Version, "payload": next.Payload, "actor_id": actor, "updated_at": next.UpdatedAt})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return status.Error(codes.Aborted, "configuration changed; reload before saving")
			}
		}
		return tx.WithContext(ctx).Create(&Change{ID: platform.ID("config-change"), DocumentID: id, Version: next.Version, ActorID: actor, Reason: strings.TrimSpace(reason), Before: old.Payload, After: string(raw)}).Error
	})
	if err != nil {
		return nil, err
	}
	return platform.Result{"id": id, "version": expected + 1, "config": value}, nil
}
