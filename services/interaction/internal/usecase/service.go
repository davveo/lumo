package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/interaction/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "interaction.usecase."+c.Action)
	defer span.End()
	switch c.Action {
	case "list":
		kind := platform.Text(c.Data, "kind")
		if kind == "" {
			kind = "review"
		}
		if kind != "review" && kind != "question" {
			return nil, bad("kind must be review or question")
		}
		owner := ""
		st := "PUBLISHED"
		if pending, _ := c.Data["include_pending"].(bool); pending {
			if e := platform.RequireAdmin(c); e != nil {
				return nil, e
			}
			st = ""
		}
		if mine, _ := c.Data["mine"].(bool); mine {
			if e := platform.RequireUser(c); e != nil {
				return nil, e
			}
			owner = c.UserID
			st = ""
		}
		data, e := s.list(ctx, c, owner, kind, st)
		if e != nil {
			return nil, e
		}
		return data, nil
	case "create":
		if e := platform.RequireUser(c); e != nil {
			return nil, e
		}
		kind := platform.Text(c.Data, "kind")
		if kind == "" {
			kind = "review"
		}
		sku := platform.Text(c.Data, "sku_id")
		body := platform.Text(c.Data, "body")
		if sku == "" || body == "" || len(body) > 4000 {
			return nil, bad("sku_id and body required")
		}
		if _, e := call(ctx, s.rpc, "catalog", child(c, "get", sku, nil)); e != nil {
			return nil, e
		}
		if kind == "review" {
			rating := platform.Int(c.Data, "rating")
			if rating < 1 || rating > 5 {
				return nil, bad("rating must be 1..5")
			}
			oid := platform.Text(c.Data, "order_id")
			o, e := call(ctx, s.rpc, "order", child(c, "get", oid, nil))
			if e != nil {
				return nil, e
			}
			if platform.Text(o, "user_id") != c.UserID || platform.Text(o, "status") != "COMPLETED" {
				return nil, status.Error(codes.FailedPrecondition, "completed owned order required")
			}
			var snap struct {
				Items []struct {
					SKU string `json:"sku_id"`
				} `json:"items"`
			}
			if e = platform.Decode(o, &snap); e != nil {
				return nil, e
			}
			found := false
			for _, it := range snap.Items {
				if it.SKU == sku {
					found = true
				}
			}
			if !found {
				return nil, bad("SKU not purchased in order")
			}
			c.ID = stable(c.UserID, "review", oid, sku)
		} else if kind == "question" {
			if c.IdempotencyKey == "" {
				return nil, bad("question idempotency_key required")
			}
			c.ID = stable(c.UserID, "question", c.IdempotencyKey)
		} else {
			return nil, bad("kind must be review or question")
		}
		r := record(c.ID, c.UserID, kind, "PENDING", map[string]any{"sku_id": sku, "body": body, "rating": platform.Int(c.Data, "rating"), "order_id": platform.Text(c.Data, "order_id")})
		r.ParentID = sku
		inserted, e := s.repo.InsertOnce(ctx, &r, nil)
		if e != nil {
			return nil, e
		}
		if !inserted {
			r, e = s.repo.Get(ctx, r.ID)
			if e != nil {
				return nil, e
			}
		}
		return view(r), nil
	case "moderate", "answer":
		if e := platform.RequireAdmin(c); e != nil {
			return nil, e
		}
		r, e := s.repo.Mutate(ctx, domain.Record{ID: c.ID}, false, func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			approved, ok := c.Data["approved"].(bool)
			if c.Action == "answer" {
				approved = true
				ok = true
				if platform.Text(c.Data, "answer") == "" {
					return nil, nil, bad("answer required")
				}
				d := payload(*v)
				d["answer"] = platform.Text(c.Data, "answer")
				v.Payload = record(v.ID, v.UserID, v.Kind, v.Status, d).Payload
			}
			if !ok {
				return nil, nil, bad("approved boolean required")
			}
			v.Status = "REJECTED"
			if approved {
				v.Status = "PUBLISHED"
			}
			evt := &platform.Event{ID: platform.ID("evt"), Type: "QAChanged", AggregateID: v.ID, UserID: v.UserID, Data: view(*v)}
			if v.Kind == "review" {
				evt.Type = "ReviewPublished"
			}
			return nil, evt, nil
		})
		if e != nil {
			return nil, e
		}
		return view(r), nil
	case "like":
		if e := platform.RequireUser(c); e != nil {
			return nil, e
		}
		target, e := s.repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if target.Status != "PUBLISHED" || target.Kind != "review" {
			return nil, status.Error(codes.FailedPrecondition, "published review required")
		}
		r := record(stable(c.UserID, "like", c.ID), c.UserID, "like", "ACTIVE", nil)
		r.ParentID = c.ID
		if _, e = s.repo.InsertOnce(ctx, &r, nil); e != nil {
			return nil, e
		}
		count, e := s.repo.Count(ctx, c.ID, "like")
		if e != nil {
			return nil, e
		}
		return platform.Result{"id": c.ID, "helpful_count": count, "liked": true}, nil
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported interaction action")
	}
}
func (s *Service) seed(context.Context) error                  { return nil }
func (s *Service) event(context.Context, platform.Event) error { return nil }
