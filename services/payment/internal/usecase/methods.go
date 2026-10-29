package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/payment/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
)

func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "payment.usecase.PolicyGet")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	p, v, e := s.r.MethodsPolicy(ctx)
	return platform.Result{"config": p, "version": v, "capabilities": paymentCapabilities()}, e
}
func paymentCapabilities() []platform.Result {
	return []platform.Result{{"id": constants.PaymentProviderDemo, "name": "演示支付", "configured": os.Getenv("DEMO_MODE") == "true" || os.Getenv("DEMO_MODE") == "1"}, {"id": "wechat", "name": "微信支付", "configured": false, "reason": "尚未接入微信支付渠道适配器"}, {"id": "alipay", "name": "支付宝", "configured": false, "reason": "尚未接入支付宝渠道适配器"}, {"id": "card", "name": "银行卡", "configured": false, "reason": "尚未接入银行卡收单渠道适配器"}}
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "payment.usecase.PolicyUpsert")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Config repository.MethodsPolicy `json:"config"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if c.Data["config"] == nil {
		return nil, status.Error(codes.InvalidArgument, "payment configuration required")
	}
	if in.Config.Wechat || in.Config.Alipay || in.Config.Card {
		return nil, status.Error(codes.FailedPrecondition, "configure a real payment adapter before enabling the corresponding channel")
	}
	if in.Config.Demo && os.Getenv("DEMO_MODE") != "true" && os.Getenv("DEMO_MODE") != "1" {
		return nil, status.Error(codes.FailedPrecondition, "demo payments require demo mode")
	}
	return s.r.SaveMethodsPolicy(ctx, c, in.Config)
}
func (s *Service) Methods(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "payment.usecase.Methods")
	defer span.End()
	p, v, e := s.r.MethodsPolicy(ctx)
	if e != nil {
		return nil, e
	}
	items := []platform.Result{}
	if p.Demo && (os.Getenv("DEMO_MODE") == "true" || os.Getenv("DEMO_MODE") == "1") {
		items = append(items, platform.Result{"id": constants.PaymentProviderDemo, "name": "演示支付", "description": "模拟支付渠道，不产生真实扣款"})
	}
	return platform.Result{"items": items, "version": v}, nil
}
func (s *Service) requireEnabledMethod(ctx context.Context, provider string) error {
	p, _, e := s.r.MethodsPolicy(ctx)
	if e != nil {
		return e
	}
	if provider == "" {
		provider = constants.PaymentProviderDemo
	}
	if provider != constants.PaymentProviderDemo || !p.Demo || (os.Getenv("DEMO_MODE") != "true" && os.Getenv("DEMO_MODE") != "1") {
		return status.Error(codes.FailedPrecondition, "该支付方式已关闭，请刷新后选择其他方式")
	}
	return nil
}
