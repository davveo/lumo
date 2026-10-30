package usecase

import (
	"context"
	"os"
	"strings"

	"github.com/davveo/lumo/internal/channelconfig"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func loginSchema(id string) (channelconfig.Schema, error) {
	f := func(key, label, kind string, required, secret bool, help string) channelconfig.Field {
		return channelconfig.Field{Key: key, Label: label, Type: kind, Required: required, Secret: secret, Help: help}
	}
	s := channelconfig.Schema{ID: id}
	switch id {
	case "wechat", "qq":
		s.Name = "微信网站登录"
		if id == "qq" {
			s.Name = "QQ网站登录"
		}
		s.Fields = []channelconfig.Field{f("app_id", "应用 App ID", "text", true, false, "填写对应开放平台的应用编号"), f("app_secret", "应用密钥 App Secret / App Key", "password", true, true, "留空保留已保存的密钥，不会回显明文"), f("redirect_uri", "授权回调地址", "url", true, false, "填写前台 index.html 的 HTTPS 地址，须与开放平台登记一致，不含 #"), f("scope", "授权范围", "select", true, false, "授权范围需与应用已申请权限一致")}
		if id == "wechat" {
			s.Fields[3].Options = []string{"snsapi_login"}
		} else {
			s.Fields[3].Options = []string{"get_user_info"}
		}
	case "phone":
		s.Name = "手机验证码短信"
		s.Fields = []channelconfig.Field{f("webhook_url", "短信发送服务地址", "url", false, false, "HTTPS webhook，成功接受短信后返回 2xx"), f("webhook_token", "短信服务访问令牌", "password", false, true, "通过 Authorization Bearer 发送"), f("sign_name", "短信签名", "text", false, false, "由短信服务适配器映射到供应商签名"), f("template_id", "短信模板编号", "text", false, false, "由短信服务适配器映射到供应商模板")}
	default:
		return s, status.Error(codes.InvalidArgument, "不支持的登录渠道")
	}
	return s, nil
}
func loginDefaults(id string) map[string]string {
	if id == "phone" {
		return map[string]string{"webhook_url": os.Getenv("SMS_WEBHOOK_URL"), "webhook_token": os.Getenv("SMS_WEBHOOK_TOKEN"), "sign_name": os.Getenv("SMS_SIGN_NAME"), "template_id": os.Getenv("SMS_TEMPLATE_ID")}
	}
	app, secret, redirect := oauthSettings(id)
	scope := "get_user_info"
	if id == "wechat" {
		scope = "snsapi_login"
	}
	return map[string]string{"app_id": app, "app_secret": secret, "redirect_uri": redirect, "scope": scope}
}
func (s *Service) providerParameters(ctx context.Context, id string) (channelconfig.Profile, error) {
	return s.repo.Provider(ctx, id, loginDefaults(id))
}
func readyLoginParameters(id string, v map[string]string) bool {
	if id == "password" {
		return true
	}
	if id == "phone" {
		return demoAuth() && v["webhook_url"] == "" || validCallback(v["webhook_url"]) && v["webhook_token"] != "" && (os.Getenv("IAM_OTP_HASH_KEY") != "" || channelconfig.CryptReady())
	}
	return v["app_id"] != "" && v["app_secret"] != "" && validCallback(v["redirect_uri"]) && (id == "wechat" && v["scope"] == "snsapi_login" || id == "qq" && v["scope"] == "get_user_info")
}
func (s *Service) providerReady(ctx context.Context, id string) (bool, error) {
	if id == "password" {
		return true, nil
	}
	p, e := s.providerParameters(ctx, id)
	if e != nil {
		return false, e
	}
	return readyLoginParameters(id, p.Values), nil
}
func (s *Service) loginCapabilities(ctx context.Context) ([]platform.Result, error) {
	items := []platform.Result{}
	for _, row := range []struct{ ID, Name string }{{"password", "账号密码"}, {"phone", "手机验证码"}, {"wechat", "微信"}, {"qq", "QQ"}} {
		ready, e := s.providerReady(ctx, row.ID)
		if e != nil {
			return nil, e
		}
		reason := ""
		if !ready {
			reason = "请填写并保存渠道参数后再启用"
		}
		items = append(items, platform.Result{"id": row.ID, "name": row.Name, "configured": ready, "reason": reason})
	}
	return items, nil
}
func (s *Service) ProviderGet(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "iam.usecase.ProviderGet")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	ids := []string{"phone", "wechat", "qq"}
	if c.ID != "" {
		ids = []string{c.ID}
	}
	items := []platform.Result{}
	for _, id := range ids {
		schema, e := loginSchema(id)
		if e != nil {
			return nil, e
		}
		p, e := s.providerParameters(ctx, id)
		if e != nil {
			return nil, e
		}
		v := channelconfig.Public(schema, p)
		v["configured"] = readyLoginParameters(id, p.Values)
		items = append(items, v)
	}
	return platform.Result{"items": items}, nil
}
func (s *Service) ProviderUpsert(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "iam.usecase.ProviderUpsert")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	schema, e := loginSchema(c.ID)
	if e != nil {
		return nil, e
	}
	current, e := s.providerParameters(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	next, e := channelconfig.Merge(schema, current, c)
	if e != nil {
		return nil, e
	}
	for _, key := range []string{"redirect_uri", "webhook_url"} {
		if v := next.Values[key]; v != "" && !validCallback(v) {
			return nil, status.Error(codes.InvalidArgument, "回调或短信服务地址必须是有效的HTTPS地址，开发环境仅允许HTTP localhost")
		}
	}
	if v := next.Values["app_id"]; strings.ContainsAny(v, " \t") {
		return nil, status.Error(codes.InvalidArgument, "应用编号不能包含空格")
	}
	policy, _, e := s.repo.LoginPolicy(ctx)
	if e != nil {
		return nil, e
	}
	enabled := c.ID == "wechat" && policy.Wechat || c.ID == "qq" && policy.QQ || c.ID == "phone" && policy.Phone
	if enabled && !readyLoginParameters(c.ID, next.Values) {
		return nil, status.Error(codes.FailedPrecondition, "请先关闭该登录方式，再清除必需参数")
	}
	r, e := s.repo.SaveProvider(ctx, schema, next, c)
	if e == nil {
		r["configured"] = readyLoginParameters(c.ID, next.Values)
	}
	return r, e
}
