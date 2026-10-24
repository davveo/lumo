package usecase

import (
	"context"
	"fmt"
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
	if c.Action == "templates" || c.Action == "templates_snapshot" {
		read := s.Repo.Templates
		if c.Action == "templates_snapshot" {
			read = s.Repo.TemplateSnapshot
		}
		out, err := read(ctx, c)
		if err != nil {
			return nil, err
		}
		check := &eligibility{rpc: s.RPC, user: c.UserID, role: c.Role}
		for _, item := range out["items"].([]platform.Result) {
			if item["claim_status"] != "available" {
				continue
			}
			t := domain.Template{MinPoints: platform.Int(item, "min_points"), MinGrowth: platform.Int(item, "min_growth")}
			if err := check.check(ctx, t); err != nil {
				item["claim_status"] = "conditions_unmet"
				if status.Code(err) == codes.Unavailable {
					item["claim_status"] = "unavailable"
				}
				item["claim_reason"], item["claimable"] = status.Convert(err).Message(), false
			}
		}
		return out, nil
	}
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Action == "list" {
		return s.Repo.List(ctx, c)
	}
	if c.Action == "claim" {
		check := &eligibility{rpc: s.RPC, user: c.UserID, role: c.Role}
		return s.Repo.Claim(ctx, c, check.check)
	}
	if c.Role != "system" && c.Role != "admin" {
		return nil, status.Error(codes.PermissionDenied, "internal coupon command required")
	}
	switch c.Action {
	case "reserve":
		return s.Repo.Reserve(ctx, c)
	case "confirm", "cancel":
		return s.Repo.Transition(ctx, c)
	case "refund":
		return s.Repo.Refund(ctx, c)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if e.Type != "RefundSucceeded" {
		return nil
	}
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}
	_, err := s.Execute(ctx, platform.Command{Action: "refund", ID: e.AggregateID, UserID: e.UserID, Role: "system", Data: data})
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
	v, err := e.rpc.Call(ctx, service, platform.Command{Action: "get", UserID: e.user, Role: e.role})
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
			e.points, e.pointsErr = e.read(ctx, "points", "available")
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
			e.growth, e.growthErr = e.read(ctx, "member", "growth")
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
