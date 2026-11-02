package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"unicode/utf8"
)

func (s *Service) StaffList(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.repo.StaffList(ctx, c)
}
func (s *Service) StaffUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Password string `json:"password"`
		Expected string `json:"expected_role"`
		Reason   string `json:"reason"`
		Active   *bool  `json:"active"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.Reason = strings.TrimSpace(in.Reason)
	if !platform.IsEmployee(in.Role) || in.Active == nil || in.Reason == "" ||
		utf8.RuneCountInString(in.Reason) > 240 ||
		c.ID == "" && !accountName.MatchString(in.Username) {
		return nil, status.Error(codes.InvalidArgument, "请选择员工角色、状态并填写操作原因和有效账号")
	}
	hash := ""
	if in.Password != "" {
		if !validNewPassword(in.Password) {
			return nil, status.Error(codes.InvalidArgument, "初始密码需包含字母和数字，长度8至72字节")
		}
		raw, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if e != nil {
			return nil, e
		}
		hash = string(raw)
	}
	return s.repo.StaffUpsert(ctx, c, in.Username, in.Role, hash, in.Expected, in.Reason, *in.Active)
}
