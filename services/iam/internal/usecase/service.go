package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"github.com/davveo/lumo/services/iam/internal/repository"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

type Service struct {
	repo      *repository.Repository
	oauthHTTP *http.Client
}

func New(r *repository.Repository) *Service          { return &Service{repo: r} }
func (s *Service) Migrate(ctx context.Context) error { return s.repo.Migrate(ctx) }

func hash(
	v string,
) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}
func token() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", status.Error(codes.Internal, "session generation failed")
	}
	return hex.EncodeToString(b), nil
}
func (s *Service) Seed(ctx context.Context) error {
	if os.Getenv("DEMO_MODE") != "true" && os.Getenv("DEMO_MODE") != "1" {
		return nil
	}
	for _, v := range []struct{ User, Password, Role string }{
		{"demo", "Lumo@2026", constants.RoleUser},
		{constants.RoleAdmin, "Admin@2026", constants.RoleAdmin},
	} {
		b, e := bcrypt.GenerateFromPassword([]byte(v.Password), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		a := domain.Account{ID: v.User, Username: v.User, PasswordHash: string(b), Role: v.Role, Active: true}
		if e = s.repo.Seed(ctx, &a); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) Login(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "iam.usecase.login")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	username := strings.ToLower(strings.TrimSpace(platform.Text(c.Data, "username")))
	password := platform.Text(c.Data, "password")
	if username == "" || len(username) > 128 || password == "" || len(password) > 1024 {
		return nil, status.Error(codes.InvalidArgument, "username and password required")
	}
	if err := s.repo.BeginPasswordLogin(ctx, username, platform.Text(c.Data, "_client_ip")); err != nil {
		return nil, err
	}
	a, e := s.repo.ByUsername(ctx, username)
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	// A valid cost-10 dummy hash makes unknown and disabled accounts do the
	// same expensive comparison, without making the dummy an accepted login.
	comparisonHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	valid := e == nil && a.Active && a.PasswordHash != ""
	if valid {
		comparisonHash = a.PasswordHash
	}
	matched := bcrypt.CompareHashAndPassword([]byte(comparisonHash), []byte(password)) == nil
	if err := s.repo.FinishPasswordLogin(ctx, username, valid && matched); err != nil {
		return nil, err
	}
	if !valid || !matched {
		platform.Log(ctx, slog.LevelWarn, "password login rejected", "reason", "invalid_credentials")
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}
	policy, _, policyErr := s.repo.LoginPolicy(ctx)
	if policyErr != nil {
		return nil, policyErr
	}
	if a.Role != constants.RoleAdmin && !platform.IsEmployee(a.Role) && !policy.Password {
		return nil, status.Error(codes.PermissionDenied, "账号密码登录已关闭，请使用其他登录方式")
	}
	raw, e := token()
	if e != nil {
		return nil, e
	}
	sess := domain.Session{
		Device:    platform.Text(c.Data, "_user_agent"),
		ClientIP:  platform.Text(c.Data, "_client_ip"),
		Digest:    hash(raw),
		UserID:    a.ID,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if e = s.repo.CreatePasswordSession(ctx, &sess, a.PasswordHash); e != nil {
		return nil, e
	}
	return platform.Result{
		"token":      raw,
		"token_type": constants.IAMTokenTypeBearer,
		"expires_at": sess.ExpiresAt.Format(time.RFC3339),
		"user_id":    a.ID,
		"role":       a.Role,
		"staff": platform.IsEmployee(
			a.Role,
		), "permissions": platform.StaffGrants(a.Role), "can_export": platform.StaffCanExport(a.Role),
		"registered_at": a.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}
func (s *Service) Validate(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "iam.usecase.validate")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	raw := platform.Text(c.Data, "token")
	if len(raw) != 64 {
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}
	sess, e := s.repo.Session(ctx, hash(raw))
	if e != nil || sess.RevokedAt != nil || !sess.ExpiresAt.After(time.Now()) {
		return nil, status.Error(codes.Unauthenticated, "session revoked or expired")
	}
	a, e := s.repo.ByID(ctx, sess.UserID)
	if e != nil || !a.Active {
		return nil, status.Error(codes.Unauthenticated, "account unavailable")
	}

	return platform.Result{
		"user_id": a.ID,
		"role":    a.Role,
		"staff": platform.IsEmployee(
			a.Role,
		), "permissions": platform.StaffGrants(a.Role), "can_export": platform.StaffCanExport(a.Role),
		"session_valid": true,
		"expires_at":    sess.ExpiresAt.Format(time.RFC3339),
		"registered_at": a.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}
func (s *Service) Refresh(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "iam.usecase.refresh")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	raw := platform.Text(c.Data, "token")
	if len(raw) != 64 {
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}
	sess, e := s.repo.Session(ctx, hash(raw))
	if e != nil || sess.RevokedAt != nil || !sess.ExpiresAt.After(time.Now()) {
		return nil, status.Error(codes.Unauthenticated, "session revoked or expired")
	}
	a, e := s.repo.ByID(ctx, sess.UserID)
	if e != nil || !a.Active {
		return nil, status.Error(codes.Unauthenticated, "account unavailable")
	}

	next, e := token()
	if e != nil {
		return nil, e
	}
	v := domain.Session{
		Digest:    hash(next),
		UserID:    a.ID,
		Device:    sess.Device,
		ClientIP:  sess.ClientIP,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if e = s.repo.Rotate(ctx, sess.Digest, &v); e != nil {
		return nil, e
	}
	return platform.Result{
		"token":      next,
		"token_type": constants.IAMTokenTypeBearer,
		"user_id":    a.ID,
		"role":       a.Role,
		"staff": platform.IsEmployee(
			a.Role,
		), "permissions": platform.StaffGrants(a.Role), "can_export": platform.StaffCanExport(a.Role),
		"expires_at":    v.ExpiresAt.Format(time.RFC3339),
		"registered_at": a.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}
func (s *Service) Logout(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "iam.usecase.logout")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	raw := platform.Text(c.Data, "token")
	if len(raw) != 64 {
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}
	sess, e := s.repo.Session(ctx, hash(raw))
	if e != nil || sess.RevokedAt != nil || !sess.ExpiresAt.After(time.Now()) {
		return nil, status.Error(codes.Unauthenticated, "session revoked or expired")
	}
	a, e := s.repo.ByID(ctx, sess.UserID)
	if e != nil || !a.Active {
		return nil, status.Error(codes.Unauthenticated, "account unavailable")
	}
	if c.UserID != "" && c.UserID != a.ID {
		return nil, status.Error(codes.PermissionDenied, "session owner mismatch")
	}
	if e = s.repo.Revoke(ctx, sess.Digest, ""); e != nil {
		return nil, e
	}
	return platform.Result{"revoked": true}, nil
}
func (s *Service) LogoutAll(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "iam.usecase.logout_all")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if e := s.repo.Revoke(ctx, "", c.UserID); e != nil {
		return nil, e
	}
	return platform.Result{"revoked_all": true}, nil
}
