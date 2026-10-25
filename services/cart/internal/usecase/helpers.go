package usecase

import (
	"github.com/davveo/lumo/internal/constants"

	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/cart/internal/domain"
	"github.com/davveo/lumo/services/cart/internal/repository"
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
func own(c platform.Request, r domain.Record) error {
	if r.UserID != c.UserID && c.Role != constants.RoleAdmin {
		return status.Error(codes.PermissionDenied, "resource belongs to another user")
	}
	return nil
}
func (s *Service) list(ctx context.Context, c platform.Request, owner, kind, st string) (platform.Result, error) {
	vs, err := s.repo.List(
		ctx,
		owner,
		kind,
		st,
		int(platform.Int(c.Data, "limit")),
		int(platform.Int(c.Data, "offset")),
	)
	if err != nil {
		return nil, err
	}
	items := make([]platform.Result, 0, len(vs))
	for _, v := range vs {
		items = append(items, view(v))
	}
	total, quantity, err := s.repo.Counts(ctx, owner, kind, st)
	if err != nil {
		return nil, err
	}
	return platform.Result{
		"items": items, "limit": len(items), "total": total, "quantity_total": quantity,
	}, nil
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
