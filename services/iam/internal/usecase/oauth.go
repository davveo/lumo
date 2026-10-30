package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func (s *Service) oauthEnabled(ctx context.Context, provider string) (bool, error) {
	p, _, e := s.repo.LoginPolicy(ctx)
	if e != nil {
		return false, e
	}
	ready, e := s.providerReady(ctx, provider)
	return ready && (provider == "wechat" && p.Wechat || provider == "qq" && p.QQ), e
}
func oauthSettings(provider string) (string, string, string) {
	if provider == "wechat" {
		return os.Getenv("WECHAT_LOGIN_APP_ID"), os.Getenv("WECHAT_LOGIN_APP_SECRET"), os.Getenv("WECHAT_LOGIN_REDIRECT_URI")
	}
	return os.Getenv("QQ_LOGIN_APP_ID"), os.Getenv("QQ_LOGIN_APP_SECRET"), os.Getenv("QQ_LOGIN_REDIRECT_URI")
}
func (s *Service) OAuthStart(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "iam.usecase.OAuthStart")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	provider, proof := platform.Text(c.Data, "provider"), platform.Text(c.Data, "proof")
	if provider != "wechat" && provider != "qq" || len(proof) < 32 || len(proof) > 128 {
		return nil, status.Error(codes.InvalidArgument, "invalid OAuth provider or browser binding")
	}
	enabled, e := s.oauthEnabled(ctx, provider)
	if e != nil {
		return nil, e
	}
	if !enabled {
		return nil, status.Error(codes.PermissionDenied, "该登录方式尚未启用或配置不完整")
	}
	state, e := token()
	if e != nil {
		return nil, e
	}
	if e = s.repo.NewOAuth(ctx, domain.AuthChallenge{ID: hash(state), Provider: provider, Binding: hash(proof), ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}, platform.Text(c.Data, "_client_ip")); e != nil {
		return nil, e
	}
	parameters, e := s.providerParameters(ctx, provider)
	if e != nil {
		return nil, e
	}
	app, redirect := parameters.Values["app_id"], parameters.Values["redirect_uri"]
	values := url.Values{"response_type": {"code"}, "redirect_uri": {redirect}, "state": {state}}
	endpoint := "https://graph.qq.com/oauth2.0/authorize"
	if provider == "wechat" {
		endpoint = "https://open.weixin.qq.com/connect/qrconnect"
		values.Set("appid", app)
		values.Set("scope", parameters.Values["scope"])
	} else {
		values.Set("client_id", app)
		values.Set("scope", parameters.Values["scope"])
	}
	target := endpoint + "?" + values.Encode()
	if provider == "wechat" {
		target += "#wechat_redirect"
	}
	return platform.Result{"authorization_url": target, "oauth_state": state, "expires_in": 300}, nil
}
func oauthJSON(ctx context.Context, client *http.Client, endpoint string, values url.Values) (map[string]any, error) {
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if e != nil {
		return nil, status.Error(codes.Internal, "invalid OAuth endpoint")
	}
	response, e := client.Do(request)
	if e != nil {
		return nil, status.Error(codes.Unavailable, "授权平台暂不可用，请重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, status.Error(codes.Unauthenticated, "授权平台未接受登录凭证")
	}
	var value map[string]any
	if e = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&value); e != nil {
		return nil, status.Error(codes.Unauthenticated, "授权平台响应无效")
	}
	if value["error"] != nil || platform.Int(value, "errcode") != 0 {
		return nil, status.Error(codes.Unauthenticated, "授权码无效或已过期")
	}
	return value, nil
}

// Only provider-verified subjects can create an identity. No browser-supplied
// OpenID, nickname, email or phone can bind or merge an existing account.
func exchangeOAuth(ctx context.Context, client *http.Client, provider, code string) (string, error) {
	app, secret, redirect := oauthSettings(provider)
	return exchangeOAuthParameters(ctx, client, provider, code, app, secret, redirect)
}
func exchangeOAuthParameters(ctx context.Context, client *http.Client, provider, code, app, secret, redirect string) (string, error) {
	values := url.Values{"grant_type": {"authorization_code"}, "code": {code}}
	if provider == "wechat" {
		values.Set("appid", app)
		values.Set("secret", secret)
		value, e := oauthJSON(ctx, client, "https://api.weixin.qq.com/sns/oauth2/access_token", values)
		if e != nil {
			return "", e
		}
		subject := platform.Text(value, "openid")
		if subject == "" || platform.Text(value, "access_token") == "" {
			return "", status.Error(codes.Unauthenticated, "微信身份校验失败")
		}
		return subject, nil
	}
	values.Set("client_id", app)
	values.Set("client_secret", secret)
	values.Set("redirect_uri", redirect)
	values.Set("fmt", "json")
	values.Set("need_openid", "1")
	value, e := oauthJSON(ctx, client, "https://graph.qq.com/oauth2.0/token", values)
	if e != nil {
		return "", e
	}
	access := platform.Text(value, "access_token")
	if access == "" {
		return "", status.Error(codes.Unauthenticated, "QQ授权无效")
	}
	// me also binds the verified token to this exact app, rather than trusting
	// a subject field copied into the callback by a client.
	identity, e := oauthJSON(ctx, client, "https://graph.qq.com/oauth2.0/me", url.Values{"access_token": {access}, "fmt": {"json"}})
	if e != nil {
		return "", e
	}
	if platform.Text(identity, "client_id") != app || platform.Text(identity, "openid") == "" {
		return "", status.Error(codes.Unauthenticated, "QQ应用身份校验失败")
	}
	return platform.Text(identity, "openid"), nil
}
func (s *Service) OAuthCallback(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, span := platform.Start(ctx, "iam.usecase.OAuthCallback")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	provider, state, proof, code := platform.Text(c.Data, "provider"), platform.Text(c.Data, "oauth_state"), platform.Text(c.Data, "proof"), platform.Text(c.Data, "code")
	if provider != "wechat" && provider != "qq" || len(state) != 64 || len(proof) < 32 || len(proof) > 128 || len(code) < 1 || len(code) > 1024 || strings.TrimSpace(code) != code {
		return nil, status.Error(codes.InvalidArgument, "授权回调参数无效")
	}
	enabled, e := s.oauthEnabled(ctx, provider)
	if e != nil {
		return nil, e
	}
	if !enabled {
		return nil, status.Error(codes.PermissionDenied, "该登录方式已关闭")
	}
	if e = s.repo.ConsumeOAuth(ctx, hash(state), provider, hash(proof)); e != nil {
		return nil, e
	}
	client := s.oauthHTTP
	if client == nil {
		client = authClient()
	}
	parameters, e := s.providerParameters(ctx, provider)
	if e != nil {
		return nil, e
	}
	subject, e := exchangeOAuthParameters(ctx, client, provider, code, parameters.Values["app_id"], parameters.Values["app_secret"], parameters.Values["redirect_uri"])
	if e != nil {
		return nil, e
	}
	if len(subject) > 160 {
		return nil, status.Error(codes.Unauthenticated, "第三方身份无效")
	}
	policy, _, e := s.repo.LoginPolicy(ctx)
	if e != nil {
		return nil, e
	}
	account, e := s.repo.IdentityAccount(ctx, provider, subject, policy.Registration)
	if e != nil {
		return nil, e
	}
	return s.issueSession(ctx, account)
}
