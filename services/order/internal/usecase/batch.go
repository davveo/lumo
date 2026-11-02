package usecase

import (
	"context"
	"github.com/davveo/lumo/services/order/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

// Missing orders are omitted so callers can distinguish gaps without N RPCs.
func (s *Service) BatchGet(ctx context.Context, actor Actor, ids []string) ([]domain.Order, error) {
	if err := requireInternalUser(actor); err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > 500 {
		return nil, status.Error(codes.InvalidArgument, "ids must contain 1..500 orders")
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 160 || strings.TrimSpace(id) != id {
			return nil, status.Error(codes.InvalidArgument, "invalid order ID")
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return s.r.GetMany(ctx, unique)
}
