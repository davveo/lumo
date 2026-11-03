package usecase

import (
	"context"
	"encoding/base64"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/media/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

type exportWriter interface {
	PutExport(context.Context, string, []byte) (domain.StoredObject, error)
}

// StoreExport is internal only. CSV objects use deterministic private keys so
// lease recovery overwrites the same result rather than leaking extra files.
func (s *Service) StoreExport(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "media.usecase.StoreExport")
	defer sp.End()
	if c.Role != constants.RoleSystem || c.UserID == "" {
		return nil, status.Error(codes.PermissionDenied, "internal export writer required")
	}
	writer, ok := s.store.(exportWriter)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "私有对象存储未配置")
	}
	if c.ID == "" || len(c.ID) > 128 {
		return nil, bad("export id required")
	}
	content, e := base64.StdEncoding.DecodeString(platform.Text(c.Data, "file_content"))
	if e != nil || len(content) < 1 || len(content) > 2<<20 {
		return nil, bad("CSV must be 1..2MiB")
	}
	expires, e := time.Parse(time.RFC3339, platform.Text(c.Data, "expires_at"))
	if e != nil || !time.Now().Before(expires) || expires.After(time.Now().Add(8*24*time.Hour)) {
		return nil, bad("invalid export expiry")
	}
	id, e := s.repo.BusinessID(ctx, "file", "export:"+c.ID, "")
	if e != nil {
		return nil, e
	}
	info, e := writer.PutExport(ctx, "private/"+id+".csv", content)
	if e != nil {
		return nil, e
	}
	r := record(
		id,
		c.UserID,
		"export",
		constants.MediaStatusReady,
		map[string]any{
			"file_name":    c.ID + ".csv",
			"content_type": "text/plain",
			"size_bytes":   len(content),
			"object_key":   "private/" + id + ".csv",
			"etag":         info.ETag,
			"expires_at":   expires.Format(time.RFC3339),
			"storage_mode": constants.MediaStorageModeMinio,
		},
	)
	if e = s.repo.Save(ctx, &r, nil); e != nil {
		return nil, e
	}
	return platform.Result{"file_id": id}, nil
}
