package usecase

import (
	"context"
	"regexp"
	"strings"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var accountName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{3,31}$`)

// Register creates only ordinary members; policy and identity come from IAM.
func (s *Service) Register(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "iam.usecase.Register")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	p, _, err := s.repo.LoginPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if !p.Registration || !p.Password {
		return nil, status.Error(codes.PermissionDenied, "商城暂未开放账号注册")
	}
	name := strings.ToLower(strings.TrimSpace(platform.Text(c.Data, "username")))
	password := platform.Text(c.Data, "password")
	if !accountName.MatchString(name) {
		return nil, status.Error(codes.InvalidArgument, "账号需为4至32位，以字母开头，可含数字、下划线、点和短横线")
	}
	letter, digit := false, false
	for _, r := range password {
		letter = letter || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		digit = digit || r >= '0' && r <= '9'
	}
	if len(password) < 8 || len(password) > 72 || !letter || !digit {
		return nil, status.Error(codes.InvalidArgument, "密码需为8至72个字节，包含字母和数字")
	}
	if err = s.repo.RegistrationAttempt(ctx, platform.Text(c.Data, "_client_ip")); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		platform.TraceError(ctx, err)

		return nil, status.Error(codes.Internal, "暂时无法注册，请稍后重试")
	}
	a := domain.Account{Username: name, PasswordHash: string(hash), Role: constants.RoleUser, Active: true}
	if err = s.repo.Register(ctx, &a); err != nil {
		return nil, err
	}
	return platform.Result{"user_id": a.ID, "username": a.Username, "role": a.Role, "registered_at": a.CreatedAt}, nil
}
