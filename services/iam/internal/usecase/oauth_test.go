package usecase

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type oauthTransport func(*http.Request) (*http.Response, error)

func (f oauthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOAuthProviderExchangeAndSingleUseCallback(t *testing.T) {
	ctx := context.Background()
	t.Setenv("DEMO_MODE", "true")
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "iam.db"))
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := db.DB()
	defer sql.Close()
	s := New(repository.New(db))
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	for _, provider := range []string{"wechat", "qq"} {
		t.Run(provider, func(t *testing.T) {
			prefix := "QQ_LOGIN_"
			if provider == "wechat" {
				prefix = "WECHAT_LOGIN_"
			}
			t.Setenv(prefix+"APP_ID", "verified-app")
			t.Setenv(prefix+"APP_SECRET", "server-only-secret")
			t.Setenv(prefix+"REDIRECT_URI", "https://shop.example/index.html")
			p, v, e := s.repo.LoginPolicy(ctx)
			if e != nil {
				t.Fatal(e)
			}
			p.QQ = true
			p.Wechat = provider == "wechat"
			if _, e = s.repo.SaveLoginPolicy(ctx, platform.Request{UserID: "admin", Data: map[string]any{"version": v, "reason": "启用已配置渠道"}}, p); e != nil {
				t.Fatal(e)
			}
			calls := 0
			s.oauthHTTP = &http.Client{Transport: oauthTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				q := r.URL.Query()
				body := ""
				if r.Method != http.MethodGet || r.URL.Scheme != "https" {
					t.Fatal(r.URL)
				}
				if provider == "wechat" {
					if r.URL.Host != "api.weixin.qq.com" || q.Get("appid") != "verified-app" || q.Get("secret") != "server-only-secret" || q.Get("code") != "server-code" {
						t.Fatal(r.URL)
					}
					body = `{"access_token":"provider-access","openid":"verified-wechat-member"}`
				} else if calls == 1 {
					if r.URL.Host != "graph.qq.com" || r.URL.Path != "/oauth2.0/token" || q.Get("client_secret") != "server-only-secret" || q.Get("redirect_uri") != "https://shop.example/index.html" {
						t.Fatal(r.URL)
					}
					body = `{"access_token":"provider-access"}`
				} else {
					if r.URL.Host != "graph.qq.com" || r.URL.Path != "/oauth2.0/me" || q.Get("access_token") != "provider-access" {
						t.Fatal(r.URL)
					}
					body = `{"client_id":"verified-app","openid":"verified-qq-member"}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			proof := strings.Repeat("b", 64)
			start, e := s.OAuthStart(ctx, platform.Request{Data: map[string]any{"provider": provider, "proof": proof, "_client_ip": "127.0.0.1"}})
			if e != nil {
				t.Fatal(e)
			}
			callback := platform.Request{Data: map[string]any{"provider": provider, "proof": proof, "oauth_state": start["oauth_state"], "code": "server-code", "openid": "forged-member", "role": "admin"}}
			result, e := s.OAuthCallback(ctx, callback)
			if e != nil {
				t.Fatal(e)
			}
			if !platform.IsBusinessID(platform.Text(result, "user_id"), "user") || platform.Text(result, "role") != "user" || platform.Text(result, "token") == "" {
				t.Fatal(result)
			}
			exchanges := calls
			if _, e = s.OAuthCallback(ctx, callback); status.Code(e) != codes.Unauthenticated || calls != exchanges {
				t.Fatal("callback replay contacted provider or logged in", e, calls)
			}
		})
	}
}

func TestQQExchangeRejectsIdentityForAnotherApplication(t *testing.T) {
	t.Setenv("QQ_LOGIN_APP_ID", "app-a")
	calls := 0
	client := &http.Client{Transport: oauthTransport(func(*http.Request) (*http.Response, error) {
		calls++
		body := `{"access_token":"provider-access"}`
		if calls == 2 {
			body = `{"client_id":"app-b","openid":"wrong-app-user"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if _, e := exchangeOAuth(context.Background(), client, "qq", "code"); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
}
