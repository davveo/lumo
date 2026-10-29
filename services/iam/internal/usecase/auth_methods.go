package usecase

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"github.com/davveo/lumo/services/iam/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var phonePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)

func demoAuth() bool { return os.Getenv("DEMO_MODE") == "true" || os.Getenv("DEMO_MODE") == "1" }
func validCallback(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || demoAuth() && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))
}
func providerReady(id string) bool {
	switch id {
	case "password":
		return true
	case "phone":
		return demoAuth() || validCallback(os.Getenv("SMS_WEBHOOK_URL")) && os.Getenv("SMS_WEBHOOK_TOKEN") != "" && os.Getenv("IAM_OTP_HASH_KEY") != ""
	case "wechat":
		return os.Getenv("WECHAT_LOGIN_APP_ID") != "" && os.Getenv("WECHAT_LOGIN_APP_SECRET") != "" && validCallback(os.Getenv("WECHAT_LOGIN_REDIRECT_URI"))
	case "qq":
		return os.Getenv("QQ_LOGIN_APP_ID") != "" && os.Getenv("QQ_LOGIN_APP_SECRET") != "" && validCallback(os.Getenv("QQ_LOGIN_REDIRECT_URI"))
	}
	return false
}
func loginCapabilities() []platform.Result {
	items := []platform.Result{}
	for _, row := range []struct{ ID, Name string }{{"password", "账号密码"}, {"phone", "手机验证码"}, {"wechat", "微信"}, {"qq", "QQ"}} {
		ready := providerReady(row.ID)
		reason := ""
		if !ready {
			reason = "请先配置该渠道的服务端凭据与回调地址"
		}
		items = append(items, platform.Result{"id": row.ID, "name": row.Name, "configured": ready, "reason": reason})
	}
	return items
}
func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "iam.usecase.PolicyGet")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	p, v, e := s.repo.LoginPolicy(ctx)
	return platform.Result{"config": p, "version": v, "capabilities": loginCapabilities()}, e
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "iam.usecase.PolicyUpsert")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Config repository.LoginPolicy `json:"config"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	p := in.Config
	if p.OTPTTL < 60 || p.OTPTTL > 600 || p.OTPCooldown < 30 || p.OTPCooldown > 300 || !p.Password && !p.Phone && !p.Wechat && !p.QQ {
		return nil, status.Error(codes.InvalidArgument, "至少启用一种登录方式，验证码有效期60至600秒，发送间隔30至300秒")
	}
	for id, enabled := range map[string]bool{"phone": p.Phone, "wechat": p.Wechat, "qq": p.QQ} {
		if enabled && !providerReady(id) {
			return nil, status.Error(codes.FailedPrecondition, "该登录渠道尚未完成服务端配置")
		}
	}
	return s.repo.SaveLoginPolicy(ctx, c, p)
}
func (s *Service) LoginMethods(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "iam.usecase.LoginMethods")
	defer span.End()
	p, v, e := s.repo.LoginPolicy(ctx)
	if e != nil {
		return nil, e
	}
	items := []platform.Result{}
	enabled := map[string]bool{"password": p.Password, "phone": p.Phone, "wechat": p.Wechat, "qq": p.QQ}
	for _, row := range loginCapabilities() {
		if enabled[platform.Text(row, "id")] && row["configured"] == true {
			items = append(items, platform.Result{"id": row["id"], "name": row["name"]})
		}
	}
	return platform.Result{"items": items, "version": v, "allow_registration": p.Registration, "otp_cooldown_seconds": p.OTPCooldown, "demo_sms": demoAuth() && os.Getenv("SMS_WEBHOOK_URL") == ""}, nil
}
func otpDigest(id, phone, code string) string {
	key := os.Getenv("IAM_OTP_HASH_KEY")
	if key == "" && demoAuth() {
		key = "lumo-isolated-demo-otp-only"
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(id + "\x00" + phone + "\x00" + code))
	return hex.EncodeToString(mac.Sum(nil))
}
func (s *Service) OtpSend(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "iam.usecase.OtpSend")
	defer span.End()
	p, _, e := s.repo.LoginPolicy(ctx)
	if e != nil {
		return nil, e
	}
	phone := strings.TrimSpace(platform.Text(c.Data, "phone"))
	if !phonePattern.MatchString(phone) {
		return nil, status.Error(codes.InvalidArgument, "请输入有效的11位手机号")
	}
	if !p.Phone || !providerReady("phone") {
		return nil, status.Error(codes.PermissionDenied, "手机验证码登录尚未启用")
	}
	id, e := token()
	if e != nil {
		return nil, e
	}
	number, e := rand.Int(rand.Reader, big.NewInt(1000000))
	if e != nil {
		return nil, status.Error(codes.Internal, "验证码生成失败")
	}
	code := fmt.Sprintf("%06d", number.Int64())
	expires := time.Now().UTC().Add(time.Duration(p.OTPTTL) * time.Second)
	if e = s.repo.NewOTP(ctx, domain.AuthChallenge{ID: id, Provider: "phone", Subject: phone, Digest: otpDigest(id, phone, code), ExpiresAt: expires}, platform.Text(c.Data, "_client_ip"), p.OTPCooldown); e != nil {
		return nil, e
	}
	out := platform.Result{"challenge_id": id, "expires_in": p.OTPTTL, "retry_after": p.OTPCooldown}
	endpoint := os.Getenv("SMS_WEBHOOK_URL")
	if endpoint == "" && demoAuth() {
		out["demo_code"] = code
	} else {
		payload, _ := json.Marshal(map[string]any{"phone": phone, "code": code, "ttl_seconds": p.OTPTTL, "request_id": id})
		request, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if e != nil {
			return nil, status.Error(codes.Unavailable, "短信通道暂不可用")
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+os.Getenv("SMS_WEBHOOK_TOKEN"))
		response, e := authClient().Do(request)
		if e != nil {
			return nil, status.Error(codes.Unavailable, "短信发送失败，请稍后重试")
		}
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, status.Error(codes.Unavailable, "短信发送失败，请稍后重试")
		}
	}
	if e = s.repo.Delivered(ctx, id); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Service) OtpLogin(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "iam.usecase.OtpLogin")
	defer span.End()
	p, _, e := s.repo.LoginPolicy(ctx)
	if e != nil {
		return nil, e
	}
	if !p.Phone || !providerReady("phone") {
		return nil, status.Error(codes.PermissionDenied, "手机验证码登录已关闭")
	}
	id, phone, code := platform.Text(c.Data, "challenge_id"), platform.Text(c.Data, "phone"), platform.Text(c.Data, "code")
	if len(id) != 64 || !phonePattern.MatchString(phone) || len(code) != 6 {
		return nil, status.Error(codes.InvalidArgument, "手机号及验证码格式无效")
	}
	account, e := s.repo.ConsumeOTP(ctx, id, phone, otpDigest(id, phone, code), p.Registration)
	if e != nil {
		return nil, e
	}
	return s.issueSession(ctx, account)
}
func (s *Service) issueSession(ctx context.Context, a domain.Account) (platform.Result, error) {
	raw, e := token()
	if e != nil {
		return nil, e
	}
	session := domain.Session{Digest: hash(raw), UserID: a.ID, ExpiresAt: time.Now().UTC().Add(24 * time.Hour)}
	if e = s.repo.CreateSession(ctx, &session); e != nil {
		return nil, e
	}
	return platform.Result{"token": raw, "token_type": "Bearer", "expires_at": session.ExpiresAt.Format(time.RFC3339), "user_id": a.ID, "role": a.Role}, nil
}
func authClient() *http.Client {
	return &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
