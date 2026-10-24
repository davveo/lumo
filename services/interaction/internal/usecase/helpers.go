package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/interaction/internal/domain"
	"github.com/davveo/lumo/services/interaction/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
	"strings"
	"time"
)

type Service struct {
	repo *repository.Repository
	rpc  platform.Caller
}

func New(r *repository.Repository, rpc platform.Caller) *Service { return &Service{repo: r, rpc: rpc} }
func (s *Service) Migrate(ctx context.Context) error             { return s.repo.Migrate(ctx) }
func bad(s string) error                                         { return status.Error(codes.InvalidArgument, s) }
func stable(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
func record(id, user, kind, state string, data map[string]any) domain.Record {
	b, _ := json.Marshal(data)
	return domain.Record{ID: id, UserID: user, Kind: kind, Status: state, Payload: string(b)}
}
func payload(r domain.Record) map[string]any {
	v := map[string]any{}
	_ = json.Unmarshal([]byte(r.Payload), &v)
	return v
}
func view(r domain.Record) platform.Result {
	v := payload(r)
	v["id"] = r.ID
	v["user_id"] = r.UserID
	v["status"] = r.Status
	v["kind"] = r.Kind
	v["created_at"] = r.CreatedAt.Format(time.RFC3339)
	return v
}
func own(c platform.Command, r domain.Record) error {
	if r.UserID != c.UserID && c.Role != constants.RoleAdmin {
		return status.Error(codes.PermissionDenied, "resource belongs to another user")
	}
	return nil
}
func (s *Service) list(ctx context.Context, c platform.Command, owner, kind, st string) (platform.Result, error) {
	skus := []string{}
	if sku := platform.Text(c.Data, "sku_id"); sku != "" {
		skus = append(skus, sku)
		product, e := call(ctx, s.rpc, constants.ServiceCatalog, child(c, constants.ActionGet, sku, nil))
		if e != nil {
			return nil, e
		}
		var p struct {
			Variants []struct {
				SKU string `json:"sku_id"`
			} `json:"variants"`
		}
		if e = platform.Decode(product, &p); e != nil {
			return nil, e
		}
		for _, v := range p.Variants {
			if v.SKU != sku {
				skus = append(skus, v.SKU)
			}
		}
	}
	vs, err := s.repo.ListSKUs(
		ctx,
		owner,
		kind,
		st,
		skus,
		int(platform.Int(c.Data, "limit")),
		int(platform.Int(c.Data, "offset")),
	)
	if err != nil {
		return nil, err
	}
	items := make([]platform.Result, 0, len(vs))
	for _, v := range vs {
		item := view(v)
		if v.Kind == constants.InteractionKindReview {
			count, e := s.repo.Count(ctx, v.ID, constants.InteractionKindLike)
			if e != nil {
				return nil, e
			}
			item["helpful_count"] = count
			if c.UserID != "" {
				liked, e := s.repo.HasLike(ctx, v.ID, c.UserID)
				if e != nil {
					return nil, e
				}
				item["liked"] = liked
			}
		}
		items = append(items, item)
	}
	return platform.Result{"items": items, "limit": len(items)}, nil
}
func call(ctx context.Context, rpc platform.Caller, target string, c platform.Command) (platform.Result, error) {
	if rpc == nil {
		return nil, status.Error(codes.Unavailable, "RPC peer unavailable")
	}
	return rpc.Call(ctx, target, c)
}
func child(c platform.Command, action, id string, data map[string]any) platform.Command {
	c.Action = action
	c.ID = id
	c.Data = data
	return c
}

func demo() bool {
	return os.Getenv(
		"DEMO_MODE",
	) == "true" || os.Getenv(
		"DEMO_MODE",
	) == "1"
}
func (s *Service) Seed(ctx context.Context) error                      { return s.seed(ctx) }
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error { return s.event(ctx, e) }
