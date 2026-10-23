package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "engagement.usecase."+c.Action)
	defer span.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = "favorite"
	}
	if kind != "favorite" && kind != "compare" && kind != "history" && kind != "search" {
		return nil, bad("kind must be favorite, compare, history or search")
	}
	switch c.Action {
	case "list":
		return s.list(ctx, c, c.UserID, kind, "")
	case "put":
		target := platform.Text(c.Data, "sku_id")
		if kind == "search" {
			target = platform.Text(c.Data, "query")
		}
		if target == "" || len(target) > 200 {
			return nil, bad("target required")
		}
		if kind != "search" {
			if _, e := call(ctx, s.rpc, "catalog", child(c, "get", target, nil)); e != nil {
				return nil, e
			}
		}
		if kind == "compare" {
			vs, e := s.repo.List(ctx, c.UserID, kind, "", 100, 0)
			if e != nil {
				return nil, e
			}
			if len(vs) >= 3 {
				found := false
				for _, v := range vs {
					if v.ID == stable(c.UserID, kind, target) {
						found = true
					}
				}
				if !found {
					return nil, status.Error(codes.ResourceExhausted, "compare list limited to three products")
				}
			}
		}
		r := record(stable(c.UserID, kind, target), c.UserID, kind, "ACTIVE", map[string]any{"sku_id": target, "query": target})
		if e := s.repo.Save(ctx, &r, nil); e != nil {
			return nil, e
		}
		return view(r), nil
	case "delete":
		id := c.ID
		if target := platform.Text(c.Data, "sku_id"); target != "" {
			id = stable(c.UserID, kind, target)
		}
		if e := s.repo.Delete(ctx, id, c.UserID); e != nil {
			return nil, e
		}
		return platform.Result{"deleted": true}, nil
	case "clear":
		if e := s.repo.DeleteKind(ctx, c.UserID, kind); e != nil {
			return nil, e
		}
		return platform.Result{"cleared": true}, nil
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported engagement action")
	}
}
func (s *Service) seed(context.Context) error                  { return nil }
func (s *Service) event(context.Context, platform.Event) error { return nil }
