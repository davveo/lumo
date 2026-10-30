package usecase

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"os"
	"strings"

	"github.com/davveo/lumo/internal/channelconfig"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func paymentSchema(id string) (channelconfig.Schema, error) {
	f := func(key, label, kind string, required, secret bool, help string) channelconfig.Field {
		return channelconfig.Field{Key: key, Label: label, Type: kind, Required: required, Secret: secret, Help: help}
	}
	s := channelconfig.Schema{ID: id}
	switch id {
	case "wechat":
		s.Name = "微信支付"
		s.Fields = []channelconfig.Field{f("app_id", "应用 App ID", "text", true, false, "商户号关联的应用编号"), f("merchant_id", "商户号 mchid", "text", true, false, "微信支付商户平台提供"), f("merchant_serial", "商户证书序列号", "text", true, false, "用于识别商户签名证书"), f("api_v3_key", "API v3 密钥", "password", true, true, "32位密钥，留空保留原值"), f("merchant_private_key", "商户 API 私钥", "textarea", true, true, "RSA 2048位及以上，支持PKCS8或PKCS1 PEM"), f("platform_key_id", "微信支付公钥 ID", "text", false, false, "使用公钥验签时填写平台公钥编号"), f("platform_public_key", "微信支付公钥 / 平台证书", "textarea", false, false, "用于验签的公开材料"), f("notify_url", "支付结果通知地址", "url", true, false, "填写已部署并在支付平台登记的HTTPS通知地址"), f("refund_notify_url", "退款结果通知地址", "url", false, false, "接收退款回执的HTTPS地址")}
	case "alipay":
		s.Name = "支付宝"
		s.Fields = []channelconfig.Field{f("app_id", "应用 App ID", "text", true, false, "支付宝开放平台的应用编号"), f("environment", "运行环境", "select", true, false, "生产与沙箱使用各自的应用密钥"), f("app_private_key", "应用私钥", "textarea", true, true, "RSA2签名，支持PEM或base64编码的RSA私钥"), f("alipay_public_key", "支付宝公钥", "textarea", true, false, "用于验证支付宝响应与回调签名"), f("notify_url", "异步通知地址", "url", true, false, "支付结果应以验签后的异步通知或查询为准"), f("return_url", "支付完成返回地址", "url", false, false, "浏览器返回商城的HTTPS地址，不作为收款凭证")}
		s.Fields[1].Options = []string{"production", "sandbox"}
	case "card":
		s.Name = "银行卡收单渠道"
		s.Fields = []channelconfig.Field{f("provider_name", "收单服务商", "text", true, false, "填写实际签约的收单机构"), f("merchant_id", "收单商户号", "text", true, false, "由对应服务商分配"), f("app_id", "接入应用编号", "text", false, false, "按服务商要求填写"), f("signing_key", "签名密钥", "password", true, true, "服务商分配的签名凭据，留空保留原值"), f("notify_url", "交易结果通知地址", "url", true, false, "填写实际部署的交易通知地址")}
	default:
		return s, status.Error(codes.InvalidArgument, "不支持的支付渠道")
	}
	return s, nil
}
func validPaymentURL(raw string) bool {
	u, e := url.Parse(raw)
	demo := os.Getenv("DEMO_MODE") == "true" || os.Getenv("DEMO_MODE") == "1"
	return e == nil && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || demo && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))
}
func rsaPrivate(raw string) bool {
	var der []byte
	if b, _ := pem.Decode([]byte(raw)); b != nil {
		der = b.Bytes
	} else {
		der, _ = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(raw), ""))
	}
	if k, e := x509.ParsePKCS1PrivateKey(der); e == nil {
		return k.N.BitLen() >= 2048 && k.Validate() == nil
	}
	if v, e := x509.ParsePKCS8PrivateKey(der); e == nil {
		if k, ok := v.(*rsa.PrivateKey); ok {
			return k.N.BitLen() >= 2048 && k.Validate() == nil
		}
	}
	return false
}
func paymentProviderPublic(schema channelconfig.Schema, p channelconfig.Profile) platform.Result {
	r := channelconfig.Public(schema, p)
	r["parameters_complete"] = r["configured"]
	r["adapter_ready"] = false
	r["configured"] = false
	r["reason"] = "参数可以保存；该渠道收款、退款和验签适配器尚未接入，暂不可启用"
	return r
}
func (s *Service) ProviderGet(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "payment.usecase.ProviderGet")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	ids := []string{"wechat", "alipay", "card"}
	if c.ID != "" {
		ids = []string{c.ID}
	}
	items := []platform.Result{}
	for _, id := range ids {
		schema, e := paymentSchema(id)
		if e != nil {
			return nil, e
		}
		p, e := s.r.Provider(ctx, id)
		if e != nil {
			return nil, e
		}
		items = append(items, paymentProviderPublic(schema, p))
	}
	return platform.Result{"items": items}, nil
}
func (s *Service) ProviderUpsert(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "payment.usecase.ProviderUpsert")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	schema, e := paymentSchema(c.ID)
	if e != nil {
		return nil, e
	}
	p, e := s.r.Provider(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	next, e := channelconfig.Merge(schema, p, c)
	if e != nil {
		return nil, e
	}
	for _, key := range []string{"notify_url", "refund_notify_url", "return_url"} {
		if v := next.Values[key]; v != "" && !validPaymentURL(v) {
			return nil, status.Error(codes.InvalidArgument, "支付通知和返回地址必须使用有效HTTPS地址")
		}
	}
	if c.ID == "wechat" {
		if v := next.Values["api_v3_key"]; v != "" && len(v) != 32 {
			return nil, status.Error(codes.InvalidArgument, "微信支付API v3密钥必须是32字节")
		}
	}
	for _, key := range []string{"merchant_private_key", "app_private_key"} {
		if v := next.Values[key]; v != "" && !rsaPrivate(v) {
			return nil, status.Error(codes.InvalidArgument, "请填写有效的2048位及以上RSA私钥")
		}
	}
	r, e := s.r.SaveProvider(ctx, schema, next, c)
	if e != nil {
		return nil, e
	}
	next.Version = platform.Int(r, "version")
	return paymentProviderPublic(schema, next), nil
}
