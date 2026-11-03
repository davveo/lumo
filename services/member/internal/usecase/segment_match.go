package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SegmentMatch is internal only; it never enumerates other customers.
func (s *Service) SegmentMatch(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "member.usecase.SegmentMatch")
	defer sp.End()
	if c.Role != constants.RoleAdmin && c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal segment check required")
	}
	row, e := s.repo.Segment(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if c.Data["validate_only"] == true {
		return platform.Result{"valid": true, "name": row.Name}, nil
	}
	if c.UserID == "" {
		return nil, bad("customer required")
	}
	result, e := platform.NewClients(
		s.rpc,
	).Analytics.CustomerStats(
		ctx,
		platform.Request{
			Role:   constants.RoleAdmin,
			UserID: c.UserID,
			Data: map[string]any{
				"customer_id":   c.UserID,
				"inactive_days": row.InactiveDays,
				"page":          1,
				"page_size":     1,
			},
		},
	)
	if e != nil {
		return nil, e
	}
	var data struct {
		Items []map[string]any `json:"items"`
	}
	if e = platform.Decode(result, &data); e != nil {
		return nil, e
	}
	match := len(data.Items) == 1 && platform.Int(data.Items[0], "paid_orders") >= row.MinOrders &&
		platform.Int(data.Items[0], "net_cents") >= row.MinNetCents
	return platform.Result{"matches": match, "segment_version": row.Version, "scope": result["scope"]}, nil
}
