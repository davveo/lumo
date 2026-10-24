package usecase

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/domain"
	"github.com/davveo/lumo/services/coupon/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

type Service struct {
	Repo *repository.Repository
	RPC  platform.Caller
}

func (s *Service) Templates(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.Templates")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	return s.readTemplates(ctx, c, s.Repo.Templates)
}

func (s *Service) TemplatesSnapshot(
	ctx context.Context,
	c platform.Request,
) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.TemplatesSnapshot")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	return s.readTemplates(ctx, c, s.Repo.TemplateSnapshot)
}

func (s *Service) TemplatesByIds(
	ctx context.Context,
	c platform.Request,
) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.TemplatesByIds")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal coupon command required")
	}
	return s.readTemplates(ctx, c, s.Repo.TemplatesByIDs)
}
func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.List")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	return s.Repo.List(ctx, c)
}
func (s *Service) Claim(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.Claim")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	check := &eligibility{rpc: s.RPC, user: c.UserID, role: c.Role}
	return s.Repo.Claim(ctx, c, check.check)
}
func (s *Service) Reserve(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.Reserve")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal coupon command required")
	}
	return s.Repo.Reserve(ctx, c)
}
func (s *Service) Confirm(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.Confirm")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal coupon command required")
	}
	return s.Repo.Confirm(ctx, c)
}
func (s *Service) Cancel(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.Cancel")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal coupon command required")
	}
	return s.Repo.Cancel(ctx, c)
}
func (s *Service) Refund(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "coupon.usecase.Refund")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal coupon command required")
	}
	return s.Repo.Refund(ctx, c)
}

func (s *Service) readTemplates(
	ctx context.Context,
	c platform.Request,
	read func(context.Context, platform.Request) (platform.Result, error),
) (platform.Result, error) {
	out, err := read(ctx, c)
	if err != nil {
		return nil, err
	}
	check := &eligibility{rpc: s.RPC, user: c.UserID, role: c.Role}
	for _, item := range out["items"].([]platform.Result) {
		if item["claim_status"] != constants.CouponClaimStatusAvailable {
			continue
		}
		t := domain.Template{
			MinPoints: platform.Int(item, "min_points"),
			MinGrowth: platform.Int(item, "min_growth"),
		}
		if err := check.check(ctx, t); err != nil {
			item["claim_status"] = constants.CouponClaimStatusConditionsUnmet
			if status.Code(err) == codes.Unavailable {
				item["claim_status"] = constants.CouponClaimStatusUnavailable
			}
			item["claim_reason"], item["claimable"] = status.Convert(err).Message(), false
		}
	}
	return out, nil
}

func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type != constants.EventRefundSucceeded {
		return nil
	}
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}
	_, err := s.Refund(
		ctx,
		platform.Request{
			ID:     e.AggregateID,
			UserID: e.UserID,
			Role:   constants.RoleSystem,
			Data:   data,
		},
	)
	return err
}

// Each request reads the authoritative balance/growth at most once. The client
// cannot supply either value. Points are an eligibility threshold, not a charge.
type eligibility struct {
	rpc                    platform.Caller
	user, role             string
	points, growth         int64
	pointsRead, growthRead bool
	pointsErr, growthErr   error
}

func (e *eligibility) read(ctx context.Context, read platform.Operation, field string) (int64, error) {
	if e.rpc == nil {
		return 0, status.Error(codes.Unavailable, "领取资格暂时无法核验，请稍后重试")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	v, err := read(ctx, platform.Request{
		UserID: e.user,
		Role:   e.role,
	})
	if err != nil {
		return 0, status.Error(codes.Unavailable, "领取资格暂时无法核验，请稍后重试")
	}
	if _, ok := v[field]; !ok {
		return 0, status.Error(codes.Unavailable, "领取资格暂时无法核验，请稍后重试")
	}
	return platform.Int(v, field), nil
}
func (e *eligibility) check(ctx context.Context, t domain.Template) error {
	if t.MinPoints > 0 {
		if !e.pointsRead {
			e.points, e.pointsErr = e.read(ctx, platform.NewClients(e.rpc).Points.Get, "available")
			e.pointsRead = true
		}
		if e.pointsErr != nil {
			return e.pointsErr
		}
		if e.points < t.MinPoints {
			return status.Error(
				codes.FailedPrecondition,
				fmt.Sprintf("需可用积分达到 %d，当前 %d（领取不扣积分）", t.MinPoints, e.points),
			)
		}
	}
	if t.MinGrowth > 0 {
		if !e.growthRead {
			e.growth, e.growthErr = e.read(ctx, platform.NewClients(e.rpc).Member.Get, "growth")
			e.growthRead = true
		}
		if e.growthErr != nil {
			return e.growthErr
		}
		if e.growth < t.MinGrowth {
			return status.Error(codes.FailedPrecondition, fmt.Sprintf("需成长值达到 %d，当前 %d", t.MinGrowth, e.growth))
		}
	}
	return nil
}
