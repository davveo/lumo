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

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.coupon."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Action == constants.ActionTemplates || c.Action == constants.ActionTemplatesSnapshot || c.Action == constants.ActionTemplatesByIds {
		if c.Action == constants.ActionTemplatesByIds && c.Role != constants.RoleSystem {
			return nil, status.Error(codes.PermissionDenied, "internal coupon command required")
		}
		read := s.Repo.Templates
		if c.Action == constants.ActionTemplatesSnapshot {
			read = s.Repo.TemplateSnapshot
		}
		if c.Action == constants.ActionTemplatesByIds {
			read = s.Repo.TemplatesByIDs
		}
		out, err := read(ctx, c)
		if err != nil {
			return nil, err
		}
		check := &eligibility{rpc: s.RPC, user: c.UserID, role: c.Role}
		for _, item := range out["items"].([]platform.Result) {
			if item["claim_status"] != constants.CouponClaimStatusAvailable {
				continue
			}
			t := domain.Template{MinPoints: platform.Int(item, "min_points"), MinGrowth: platform.Int(item, "min_growth")}
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
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Action == constants.ActionList {
		return s.Repo.List(ctx, c)
	}
	if c.Action == constants.ActionClaim {
		check := &eligibility{rpc: s.RPC, user: c.UserID, role: c.Role}
		return s.Repo.Claim(ctx, c, check.check)
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal coupon command required")
	}
	switch c.Action {
	case constants.ActionReserve:
		return s.Repo.Reserve(ctx, c)
	case constants.ActionConfirm, constants.ActionCancel:
		return s.Repo.Transition(ctx, c)
	case constants.ActionRefund:
		return s.Repo.Refund(ctx, c)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type != constants.EventRefundSucceeded {
		return nil
	}
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}
	_, err := s.Execute(ctx, platform.Command{Action: constants.ActionRefund, ID: e.AggregateID, UserID: e.UserID, Role: constants.RoleSystem, Data: data})
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

func (e *eligibility) read(ctx context.Context, service, field string) (int64, error) {
	if e.rpc == nil {
		return 0, status.Error(codes.Unavailable, "领取资格暂时无法核验，请稍后重试")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	v, err := e.rpc.Call(ctx, service, platform.Command{Action: constants.ActionGet, UserID: e.user, Role: e.role})
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
			e.points, e.pointsErr = e.read(ctx, constants.ServicePoints, "available")
			e.pointsRead = true
		}
		if e.pointsErr != nil {
			return e.pointsErr
		}
		if e.points < t.MinPoints {
			return status.Error(codes.FailedPrecondition, fmt.Sprintf("需可用积分达到 %d，当前 %d（领取不扣积分）", t.MinPoints, e.points))
		}
	}
	if t.MinGrowth > 0 {
		if !e.growthRead {
			e.growth, e.growthErr = e.read(ctx, constants.ServiceMember, "growth")
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
