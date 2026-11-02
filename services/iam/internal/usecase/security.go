package usecase

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"math/big"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func validNewPassword(value string) bool {
	letter, digit := false, false
	for _, r := range value {
		letter = letter || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		digit = digit || r >= '0' && r <= '9'
	}
	return len(value) >= 8 && len(value) <= 72 && letter && digit
}
func maskedContact(channel, value string) string {
	if channel == "phone" && len(value) == 11 {
		return value[:3] + "****" + value[7:]
	}
	if at := strings.IndexByte(value, '@'); at > 0 {
		return value[:1] + "***" + value[at:]
	}
	return "***"
}
func securityDigest(id, code string) string {
	sum := sha256.Sum256([]byte(id + "\x00" + code))
	return hex.EncodeToString(sum[:])
}
func (s *Service) SecurityGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	a, e := s.repo.ByID(ctx, c.UserID)
	if e != nil {
		return nil, e
	}
	identities, e := s.repo.SecurityIdentities(ctx, c.UserID)
	if e != nil {
		return nil, e
	}
	contacts := []platform.Result{}
	for _, v := range identities {
		contacts = append(
			contacts,
			platform.Result{"channel": v.Provider, "address": maskedContact(v.Provider, v.Subject)},
		)
	}
	sessions, e := s.repo.SecuritySessions(ctx, c.UserID, platform.Text(c.Data, "_current_session"))
	return platform.Result{
		"username":     a.Username,
		"has_password": a.PasswordHash != "",
		"contacts":     contacts,
		"sessions":     sessions,
	}, e
}
func (s *Service) PasswordChange(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	next := platform.Text(c.Data, "new_password")
	if !validNewPassword(next) {
		return nil, status.Error(codes.InvalidArgument, "新密码需为8至72字节，包含字母和数字")
	}
	a, e := s.repo.ByID(ctx, c.UserID)
	if e != nil {
		return nil, e
	}
	if e = s.repo.BeginPasswordLogin(ctx, a.Username, platform.Text(c.Data, "_client_ip")); e != nil {
		return nil, e
	}
	matched := bcrypt.CompareHashAndPassword(
		[]byte(a.PasswordHash),
		[]byte(platform.Text(c.Data, "current_password")),
	) == nil
	if e = s.repo.FinishPasswordLogin(ctx, a.Username, matched); e != nil {
		return nil, e
	}
	if !matched {
		return nil, status.Error(codes.InvalidArgument, "当前密码不正确")
	}
	password, e := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if e != nil {
		return nil, e
	}
	e = s.repo.ChangePassword(ctx, a.ID, a.PasswordHash, string(password))
	return platform.Result{"changed": e == nil, "login_required": true}, e
}
func (s *Service) SessionRevoke(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	e := s.repo.RevokeSecuritySession(ctx, c.UserID, c.ID)
	return platform.Result{"revoked": e == nil}, e
}
func (s *Service) SecuritySend(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	a, e := s.repo.ByID(ctx, c.UserID)
	if e != nil {
		return nil, e
	}
	if a.PasswordHash != "" {
		if e = s.repo.BeginPasswordLogin(ctx, a.Username, platform.Text(c.Data, "_client_ip")); e != nil {
			return nil, e
		}
		matched := bcrypt.CompareHashAndPassword(
			[]byte(a.PasswordHash),
			[]byte(platform.Text(c.Data, "current_password")),
		) == nil
		if e = s.repo.FinishPasswordLogin(ctx, a.Username, matched); e != nil {
			return nil, e
		}
		if !matched {
			return nil, status.Error(codes.InvalidArgument, "当前密码不正确")
		}
	}
	channel, address := platform.Text(
		c.Data,
		"channel",
	), strings.ToLower(
		strings.TrimSpace(platform.Text(c.Data, "address")),
	)
	if channel == "phone" {
		if !phonePattern.MatchString(address) {
			return nil, status.Error(codes.InvalidArgument, "手机号格式不正确")
		}
	} else if channel == "email" {
		v, err := mail.ParseAddress(address)
		if err != nil || v.Address != address || len(address) > 160 {
			return nil, status.Error(codes.InvalidArgument, "邮箱格式不正确")
		}
	} else {
		return nil, status.Error(codes.InvalidArgument, "请选择手机或邮箱")
	}
	return s.sendSecurity(ctx, c, "bind", a.ID, channel, address, true)
}
func (s *Service) PasswordResetRequest(ctx context.Context, c platform.Request) (platform.Result, error) {
	name := strings.ToLower(strings.TrimSpace(platform.Text(c.Data, "username")))
	channel := platform.Text(c.Data, "channel")
	if !accountName.MatchString(name) || channel != "phone" && channel != "email" {
		return nil, status.Error(codes.InvalidArgument, "请输入账号并选择找回方式")
	}
	a, e := s.repo.ByUsername(ctx, name)
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	if e != nil || !a.Active {
		return s.sendSecurity(ctx, c, "reset", "", channel, name, false)
	}
	identities, e := s.repo.SecurityIdentities(ctx, a.ID)
	if e != nil {
		return nil, e
	}
	address, account, known := name, "", false
	for _, v := range identities {
		if v.Provider == channel {
			address, account, known = v.Subject, a.ID, true
			break
		}
	}
	return s.sendSecurity(ctx, c, "reset", account, channel, address, known)
}

func (s *Service) sendSecurity(
	ctx context.Context,
	c platform.Request,
	purpose, user, channel, address string,
	known bool,
) (platform.Result, error) {
	id, e := token()
	if e != nil {
		return nil, e
	}
	n, e := rand.Int(rand.Reader, big.NewInt(1000000))
	if e != nil {
		return nil, e
	}
	code := fmt.Sprintf("%06d", n.Int64())
	row := domain.SecurityChallenge{
		ID:        id,
		AccountID: user,
		Purpose:   purpose,
		Channel:   channel,
		Subject:   address,
		Digest:    securityDigest(id, code),
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}
	if e = s.repo.NewSecurityChallenge(ctx, &row, platform.Text(c.Data, "_client_ip")); e != nil {
		return nil, e
	}
	out := platform.Result{
		"challenge_id": id,
		"expires_in":   600,
		"retry_after":  60,
		"message":      "若账号已绑定该联系方式，验证码将发送至绑定地址",
	}
	if !known {
		return out, nil
	}
	endpoint, credential := os.Getenv("IAM_EMAIL_WEBHOOK_URL"), os.Getenv("IAM_EMAIL_WEBHOOK_TOKEN")
	if channel == "phone" {
		p, err := s.providerParameters(ctx, "phone")
		if err != nil {
			return nil, err
		}
		endpoint, credential = p.Values["webhook_url"], p.Values["webhook_token"]
	}
	if demoAuth() && endpoint == "" {
		out["demo_code"] = code
	} else {
		if !validCallback(endpoint) || credential == "" {
			if purpose == "reset" {
				return out, nil
			}
			return nil, status.Error(codes.FailedPrecondition, "验证码发送渠道尚未配置")
		}
		payload, _ := json.Marshal(map[string]any{"recipient": address, "phone": address, "code": code, "purpose": purpose, "ttl_seconds": 600, "request_id": id})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+credential)
		response, err := authClient().Do(req)
		if err != nil {
			if purpose == "reset" {
				return out, nil
			}
			return nil, status.Error(codes.Unavailable, "验证码发送失败")
		}
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			if purpose == "reset" {
				return out, nil
			}
			return nil, status.Error(codes.Unavailable, "验证码发送失败")
		}
	}
	e = s.repo.SecurityDelivered(ctx, id)
	return out, e
}
func (s *Service) SecurityVerify(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	id, code := platform.Text(c.Data, "challenge_id"), platform.Text(c.Data, "code")
	if len(id) != 64 || len(code) != 6 {
		return nil, status.Error(codes.InvalidArgument, "请输入有效的验证码")
	}
	e := s.repo.ConsumeSecurity(ctx, id, securityDigest(id, code), "bind", c.UserID, "")
	return platform.Result{"verified": e == nil}, e
}
func (s *Service) PasswordReset(ctx context.Context, c platform.Request) (platform.Result, error) {
	id, code, next := platform.Text(
		c.Data,
		"challenge_id",
	), platform.Text(
		c.Data,
		"code",
	), platform.Text(
		c.Data,
		"new_password",
	)
	if len(id) != 64 || len(code) != 6 || !validNewPassword(next) {
		return nil, status.Error(codes.InvalidArgument, "请输入有效验证码和8至72字节的新密码，需包含字母和数字")
	}
	password, e := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if e != nil {
		return nil, e
	}
	e = s.repo.ConsumeSecurity(ctx, id, securityDigest(id, code), "reset", "", string(password))
	return platform.Result{"changed": e == nil, "login_required": true}, e
}
