package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/member/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "member.usecase."+c.Action)
	defer span.End()
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
	switch c.Action {
	case "get", "update":
		seed := record(id, id, "profile", "ACTIVE", map[string]any{"display_name": id, "avatar": "", "preferences": map[string]any{}})
		r, e := s.repo.Mutate(ctx, seed, true, func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			if c.Action == "update" {
				d := payload(*v)
				for k, x := range c.Data {
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
			return nil, nil, nil
		})
		if e != nil {
			return nil, e
		}
		out := view(r)
		growth, e := s.repo.Sum(ctx, id, "growth")
		if e != nil {
			return nil, e
		}
		out["growth"] = growth
		if growth >= 1000 {
			out["level"] = "gold"
		} else {
			out["level"] = "member"
		}
		return out, nil
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported member action")
	}
}
func (s *Service) seed(ctx context.Context) error { return nil }
func (s *Service) event(ctx context.Context, e platform.Event) error {
	ctx, span := platform.Start(ctx, "member.usecase.OrderCompleted")
	defer span.End()
	if e.Type != "OrderCompleted" || e.UserID == "" {
		return nil
	}
	r := record(stable(e.ID, e.UserID), e.UserID, "growth", "POSTED", map[string]any{"order_id": e.AggregateID})
	r.Amount = platform.Int(e.Data, "payable_cents") / 10000
	if r.Amount < 1 {
		r.Amount = 1
	}
	_, err := s.repo.InsertOnce(ctx, &r, nil)
	return err
}
