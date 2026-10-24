package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/group/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Repository struct{ DB *gorm.DB }

func (r *Repository) Migrate(ctx context.Context) error {
	return r.DB.WithContext(ctx).AutoMigrate(&domain.Team{}, &domain.Seat{})
}
func (r *Repository) Seed(ctx context.Context) error {
	return r.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&domain.Team{ID: "group-demo", SKUID: "sku-headphones", CreatorID: "demo", Required: 2, State: constants.GroupStatusOpen, ExpiresAt: time.Now().Add(24 * time.Hour)}).Error
}
func (r *Repository) List(ctx context.Context) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.group.List")
	defer s.End()
	var v []domain.Team
	e := r.DB.WithContext(ctx).Order("created_at DESC").Limit(100).Find(&v).Error
	return platform.Result{"items": v}, e
}
func (r *Repository) Get(ctx context.Context, id string) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.group.Get")
	defer s.End()
	var t domain.Team
	if e := r.DB.WithContext(ctx).First(&t, "id = ?", id).Error; e != nil {
		return nil, status.Error(codes.NotFound, "group not found")
	}
	out := platform.Map(t)
	out["formed"] = t.State == constants.GroupStatusFormed
	return out, nil
}
func (r *Repository) Create(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.group.Create")
	defer s.End()
	required := platform.Int(c.Data, "required")
	if required == 0 {
		required = 2
	}
	sku := platform.Text(c.Data, "sku_id")
	if sku == "" || required < 2 || required > 10 {
		return nil, status.Error(codes.InvalidArgument, "SKU and required count 2..10 required")
	}
	id := c.ID
	if id == "" {
		id = platform.ID("group")
	}
	if c.IdempotencyKey != "" {
		id = "group:" + c.UserID + ":" + c.IdempotencyKey
		if len(id) > 120 {
			return nil, status.Error(codes.InvalidArgument, "idempotency key too long")
		}
	}
	t := domain.Team{ID: id, SKUID: sku, CreatorID: c.UserID, Required: required, State: constants.GroupStatusOpen, ExpiresAt: time.Now().Add(30 * time.Minute)}
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old domain.Team
		er := tx.WithContext(ctx).First(&old, "id = ?", id).Error
		if er == nil {
			if old.CreatorID != c.UserID {
				return status.Error(codes.PermissionDenied, "group owner mismatch")
			}
			if old.SKUID != sku || old.Required != required {
				return status.Error(codes.AlreadyExists, "group request differs")
			}
			t = old
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		return tx.WithContext(ctx).Create(&t).Error
	})
	return platform.Map(t), e
}
func (r *Repository) Reserve(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.group.Reserve")
	defer s.End()
	gid := platform.Text(c.Data, "group_id")
	if c.ID == "" || gid == "" {
		return nil, status.Error(codes.InvalidArgument, "order and group IDs required")
	}
	var seat domain.Seat
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&seat, "id = ?", c.ID).Error
		if er == nil {
			if seat.UserID != c.UserID {
				return status.Error(codes.PermissionDenied, "seat owner mismatch")
			}
			if seat.State == constants.GroupSeatStateCancelled {
				return status.Error(codes.FailedPrecondition, "seat cancelled")
			}
			if seat.GroupID != gid {
				return status.Error(codes.AlreadyExists, "seat request differs")
			}
			return nil
		}
		if !errors.Is(er, gorm.ErrRecordNotFound) {
			return er
		}
		var t domain.Team
		if er = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, "id = ?", gid).Error; er != nil {
			return status.Error(codes.NotFound, "group not found")
		}
		if t.State != constants.GroupStatusOpen || time.Now().After(t.ExpiresAt) {
			return status.Error(codes.FailedPrecondition, "group not open")
		}
		var prior int64
		if er = tx.WithContext(ctx).Model(&domain.Seat{}).Where("group_id = ? AND user_id = ? AND state <> ?", gid, c.UserID, constants.GroupSeatStateCancelled).Count(&prior).Error; er != nil {
			return er
		}
		if prior > 0 {
			return status.Error(codes.AlreadyExists, "user already joined group")
		}
		seat = domain.Seat{ID: c.ID, GroupID: gid, UserID: c.UserID, State: constants.GroupSeatStateReserved}
		if er = tx.WithContext(ctx).Create(&seat).Error; er != nil {
			return er
		}
		u := tx.WithContext(ctx).Model(&t).Where("occupied < required AND state = ?", constants.GroupStatusOpen).Update("occupied", gorm.Expr("occupied + 1"))
		if u.Error != nil {
			return u.Error
		}
		if u.RowsAffected != 1 {
			return status.Error(codes.ResourceExhausted, "group seats full")
		}
		return nil
	})
	return platform.Map(seat), e
}
func emit(ctx context.Context, tx *gorm.DB, t domain.Team, kind string) error {
	var seats []domain.Seat
	if e := tx.WithContext(ctx).Where("group_id = ? AND state IN ?", t.ID, []string{constants.GroupSeatStatePaid, constants.GroupSeatStateLatePaid}).Find(&seats).Error; e != nil {
		return e
	}
	for _, s := range seats {
		if e := platform.Enqueue(ctx, tx, platform.Event{ID: eventID(t.ID, kind, s.ID), Type: kind, AggregateID: s.ID, UserID: s.UserID, Data: map[string]any{"group_id": t.ID, "order_id": s.ID, "status": t.State}}); e != nil {
			return e
		}
	}
	return nil
}
func (r *Repository) Paid(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.group.Paid")
	defer s.End()
	var seat domain.Seat
	var t domain.Team
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&seat, "id = ?", c.ID).Error
		if er != nil {
			if !errors.Is(er, gorm.ErrRecordNotFound) {
				return er
			}
			return status.Error(codes.NotFound, "group seat not found")
		}
		if seat.UserID != c.UserID {
			return status.Error(codes.PermissionDenied, "seat owner mismatch")
		}
		if er = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, "id = ?", seat.GroupID).Error; er != nil {
			return er
		}
		if seat.State == constants.GroupSeatStatePaid || seat.State == constants.GroupSeatStateLatePaid {
			return nil
		}
		if seat.State != constants.GroupSeatStateReserved {
			return status.Error(codes.FailedPrecondition, "seat not reserved")
		}
		if t.State == constants.GroupStatusFailed || time.Now().After(t.ExpiresAt) {
			if t.State == constants.GroupStatusOpen {
				if er = tx.WithContext(ctx).Model(&t).Update("state", constants.GroupStatusFailed).Error; er != nil {
					return er
				}
				t.State = constants.GroupStatusFailed
				if er = emit(ctx, tx, t, constants.EventGroupFailed); er != nil {
					return er
				}
			}
			if er = tx.WithContext(ctx).Model(&seat).Update("state", constants.GroupSeatStateLatePaid).Error; er != nil {
				return er
			}
			return platform.Enqueue(ctx, tx, platform.Event{ID: eventID(t.ID, constants.EventGroupFailed, seat.ID), Type: constants.EventGroupFailed, AggregateID: seat.ID, UserID: seat.UserID, Data: map[string]any{"group_id": t.ID, "order_id": seat.ID, "status": constants.GroupStatusFailed}})
		}
		if t.State != constants.GroupStatusOpen {
			return status.Error(codes.FailedPrecondition, "group closed")
		}
		if er = tx.WithContext(ctx).Model(&seat).Update("state", constants.GroupSeatStatePaid).Error; er != nil {
			return er
		}
		t.Paid++
		next := constants.GroupStatusOpen
		if t.Paid >= t.Required {
			next = constants.GroupStatusFormed
		}
		if er = tx.WithContext(ctx).Model(&t).Updates(map[string]any{"paid": t.Paid, "state": next}).Error; er != nil {
			return er
		}
		t.State = next
		if next == constants.GroupStatusFormed {
			return emit(ctx, tx, t, constants.EventGroupFormed)
		}
		return nil
	})
	out := platform.Map(t)
	out["formed"] = t.State == constants.GroupStatusFormed
	return out, e
}
func (r *Repository) Cancel(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.group.Cancel")
	defer s.End()
	if c.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "order required")
	}
	var seat domain.Seat
	e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&seat, "id = ?", c.ID).Error
		if errors.Is(er, gorm.ErrRecordNotFound) {
			seat = domain.Seat{ID: c.ID, UserID: c.UserID, State: constants.GroupSeatStateCancelled}
			return tx.WithContext(ctx).Create(&seat).Error
		}
		if er != nil {
			return er
		}
		if seat.UserID != c.UserID {
			return status.Error(codes.PermissionDenied, "seat owner mismatch")
		}
		if seat.State == constants.GroupSeatStateCancelled {
			return nil
		}
		if seat.State != constants.GroupSeatStateReserved {
			return status.Error(codes.FailedPrecondition, "paid seat requires group/refund workflow")
		}
		if er = tx.WithContext(ctx).Model(&domain.Team{}).Where("id = ? AND occupied > 0", seat.GroupID).Update("occupied", gorm.Expr("occupied - 1")).Error; er != nil {
			return er
		}
		if er = tx.WithContext(ctx).Model(&seat).Update("state", constants.GroupSeatStateCancelled).Error; er != nil {
			return er
		}
		seat.State = constants.GroupSeatStateCancelled
		return nil
	})
	return platform.Map(seat), e
}
func (r *Repository) Expire(ctx context.Context) (platform.Result, error) {
	ctx, s := platform.Start(ctx, "repository.group.Expire")
	defer s.End()
	var teams []domain.Team
	if e := r.DB.WithContext(ctx).Where("state = ? AND expires_at <= ?", constants.GroupStatusOpen, time.Now()).Limit(100).Find(&teams).Error; e != nil {
		return nil, e
	}
	count := 0
	for _, team := range teams {
		e := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var t domain.Team
			if er := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, "id = ?", team.ID).Error; er != nil {
				return er
			}
			if t.State != constants.GroupStatusOpen || t.ExpiresAt.After(time.Now()) {
				return nil
			}
			if er := tx.WithContext(ctx).Model(&t).Update("state", constants.GroupStatusFailed).Error; er != nil {
				return er
			}
			t.State = constants.GroupStatusFailed
			if er := emit(ctx, tx, t, constants.EventGroupFailed); er != nil {
				return er
			}
			count++
			return nil
		})
		if e != nil {
			return nil, e
		}
	}
	return platform.Result{"expired": count}, nil
}
func (r *Repository) HasSeat(ctx context.Context, id string) (bool, error) {
	ctx, span := platform.Start(ctx, "repository.group.HasSeat")
	defer span.End()
	var n int64
	e := r.DB.WithContext(ctx).Model(&domain.Seat{}).Where("id = ?", id).Count(&n).Error
	return n > 0, e
}

func eventID(team, kind, order string) string {
	sum := sha256.Sum256([]byte(team + "\x00" + kind + "\x00" + order))
	return "grp-" + hex.EncodeToString(sum[:])
}
