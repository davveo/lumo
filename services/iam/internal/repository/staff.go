package repository

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *Repository) StaffList(ctx context.Context, c platform.Request) (platform.Result, error) {
	rows, total, f, e := adminquery.Page[domain.Account](
		ctx,
		r.db.Where("role IN ?", platform.StaffRoles()),
		c.Data,
		managementSpec,
	)
	out := adminquery.Result(rows, total, f)
	out["roles"] = platform.StaffRoles()
	return out, e
}

func (r *Repository) StaffUpsert(
	ctx context.Context,
	c platform.Request,
	name, role, password, expected, reason string,
	active bool,
) (platform.Result, error) {
	var out domain.Account
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"})
		if c.ID != "" {
			query = query.Where("id = ?", c.ID)
		} else {
			query = query.Where("username = ?", name)
		}
		e := query.First(&out).Error
		old := ""
		if errors.Is(e, gorm.ErrRecordNotFound) {
			if c.ID != "" || password == "" {
				return status.Error(codes.InvalidArgument, "新增员工需提供账号和初始密码")
			}
			id, err := platform.NextBusinessID(ctx, tx, "user")
			if err != nil {
				return err
			}
			out = domain.Account{ID: id, Username: name, PasswordHash: password, Role: role, Active: active}
			if err = tx.Create(&out).Error; err != nil {
				return err
			}
		} else {
			if e != nil {
				return e
			}
			old = out.Role
			if out.ID == c.UserID || out.Role == "admin" {
				return status.Error(codes.PermissionDenied, "不能通过员工管理修改自己或超级管理员")
			}
			if expected == "" || expected != out.Role {
				return status.Error(codes.Aborted, "员工角色已变化，请刷新后重试")
			}
			updates := map[string]any{"role": role, "active": active}
			if password != "" {
				updates["password_hash"] = password
			}
			if e = tx.Model(&out).Updates(updates).Error; e != nil {
				return e
			}
			out.Role = role
			out.Active = active
			if e = tx.Model(&domain.Session{}).Where("user_id = ? AND revoked_at IS NULL", out.ID).Update("revoked_at", time.Now().UTC()).Error; e != nil {
				return e
			}
		}
		return tx.Create(
			&domain.StaffChange{
				ID:      platform.ID("staff-change"),
				UserID:  out.ID,
				ActorID: c.UserID,
				OldRole: old,
				NewRole: role,
				Active:  active,
				Reason:  reason,
			},
		).Error
	})
	return platform.Map(out), err
}
