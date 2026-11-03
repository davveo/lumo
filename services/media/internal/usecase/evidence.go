package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

// VerifyFiles checks one owned metadata snapshot. Download revalidates storage
// integrity; there is no per-file object-store operation in this batch read.
func (s *Service) VerifyFiles(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal file verification required")
	}
	var input struct {
		IDs []string `json:"ids"`
	}
	if e := platform.Decode(c.Data, &input); e != nil {
		return nil, e
	}
	if len(input.IDs) < 1 || len(input.IDs) > 5 {
		return nil, bad("evidence must contain 1..5 files")
	}
	seen := map[string]bool{}
	for _, id := range input.IDs {
		if id == "" || len(id) > 128 || seen[id] {
			return nil, bad("invalid or duplicate evidence file")
		}
		seen[id] = true
	}
	rows, e := s.repo.GetManyOwned(ctx, c.UserID, input.IDs)
	if e != nil {
		return nil, e
	}
	if len(rows) != len(input.IDs) {
		return nil, status.Error(codes.PermissionDenied, "evidence file is not owned by this member")
	}
	items := []platform.Result{}
	for _, r := range rows {
		p := payload(r)
		if r.Status != constants.MediaStatusReady || !strings.HasPrefix(platform.Text(p, "content_type"), "image/") {
			return nil, status.Error(codes.FailedPrecondition, "凭证须为已完成上传的图片")
		}
		items = append(
			items,
			platform.Result{
				"file_id":      r.ID,
				"file_name":    platform.Text(p, "file_name"),
				"content_type": platform.Text(p, "content_type"),
				"etag":         platform.Text(p, "etag"),
			},
		)
	}
	return platform.Result{"items": items}, nil
}
