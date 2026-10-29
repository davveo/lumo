package usecase

import (
	"context"
	"strings"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/member/internal/domain"
)

func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "member.usecase.get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	id := c.UserID
	if c.ID != "" && c.ID != c.UserID {
		if e := platform.RequireAdmin(c); e != nil {
			return nil, e
		}
		id = c.ID
	}

	seed := record(
		id,
		id,
		constants.MemberKindProfile,
		constants.MemberStatusActive,
		map[string]any{"display_name": id, "avatar": "", "preferences": map[string]any{}},
	)
	r, e := s.repo.Mutate(ctx, seed, true, func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
		return nil, nil, nil
	})
	if e != nil {
		return nil, e
	}
	out := view(r)
	growth, e := s.repo.Sum(ctx, id, constants.MemberKindGrowth)
	if e != nil {
		return nil, e
	}
	out["growth"] = growth
	policy, err := s.repo.Policy(ctx)
	if err != nil {
		return nil, err
	}
	if growth >= policy.GoldGrowth {
		out["level"] = constants.MemberLevelGold
	} else {
		out["level"] = constants.MemberLevelMember
	}
	return out, nil
}
func (s *Service) Update(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "member.usecase.update")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	id := c.UserID
	if c.ID != "" && c.ID != c.UserID {
		if e := platform.RequireAdmin(c); e != nil {
			return nil, e
		}
		id = c.ID
	}

	seed := record(
		id,
		id,
		constants.MemberKindProfile,
		constants.MemberStatusActive,
		map[string]any{"display_name": id, "avatar": "", "preferences": map[string]any{}},
	)
	if c.Role == constants.RoleAdmin && c.ID != "" && c.ID != c.UserID {
		reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
		if reason == "" || len([]rune(reason)) > 240 {
			return nil, bad("administrator profile changes require a reason")
		}
	}
	r, e := s.repo.Mutate(ctx, seed, true, func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
		{
			d := payload(*v)
			for k, x := range c.Data {
				if k == "reason" && c.Role == constants.RoleAdmin {
					continue
				}
				if k == "display_name" {
					value, ok := x.(string)
					if !ok || strings.TrimSpace(value) == "" || len([]rune(value)) > 100 {
						return nil, nil, bad("nickname must contain 1..100 characters")
					}
				}
				if k == "avatar" {
					value, ok := x.(string)
					if !ok || len(value) > 2000 || (value != "" && !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "assets/")) {
						return nil, nil, bad("avatar must be an HTTPS or existing asset URL")
					}
				}

				switch k {
				case "display_name", "avatar", "preferences":
					d[k] = x
				default:
					return nil, nil, bad("unsupported profile field")
				}
			}
			next := record(v.ID, v.UserID, v.Kind, v.Status, d)
			v.Payload = next.Payload
		}
		if c.Role == constants.RoleAdmin && c.ID != "" && c.ID != c.UserID {
			change := record(platform.ID("profile-change"), id, "profile_change", "RECORDED", map[string]any{"actor_id": c.UserID, "reason": platform.Text(c.Data, "reason"), "fields": c.Data})
			return []domain.Record{change}, nil, nil
		}
		return nil, nil, nil
	})
	if e != nil {
		return nil, e
	}
	out := view(r)
	growth, e := s.repo.Sum(ctx, id, constants.MemberKindGrowth)
	if e != nil {
		return nil, e
	}
	out["growth"] = growth
	policy, err := s.repo.Policy(ctx)
	if err != nil {
		return nil, err
	}
	if growth >= policy.GoldGrowth {
		out["level"] = constants.MemberLevelGold
	} else {
		out["level"] = constants.MemberLevelMember
	}
	return out, nil
}

func (s *Service) seed(ctx context.Context) error { return nil }
func (s *Service) event(ctx context.Context, e platform.Event) (traceErr error) {
	ctx, span := platform.Start(ctx, "member.usecase.OrderCompleted")
	defer span.End()
	platform.TraceRequest(ctx, e)
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
	if e.Type != constants.EventOrderCompleted || e.UserID == "" {
		return nil
	}
	r := record(
		stable(e.ID, e.UserID),
		e.UserID,
		constants.MemberKindGrowth,
		constants.MemberStatusPosted,
		map[string]any{"order_id": e.AggregateID},
	)
	r.Amount = platform.Int(e.Data, "payable_cents") / 10000
	if _, ok := e.Data["growth_points"]; ok {
		r.Amount = platform.Int(e.Data, "growth_points")
	}
	if r.Amount < 1 {
		r.Amount = 1
	}
	_, err := s.repo.InsertOnce(ctx, &r, nil)
	return err
}
