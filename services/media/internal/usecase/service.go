package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/media/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"mime"
	"strings"
	"time"
)

const maxUploadSize int64 = 10 * 1024 * 1024
const signedTTL = 10 * time.Minute

var allowedMIME = map[string]bool{
	constants.MediaContentTypeJPEG:      true,
	constants.MediaContentTypePNG:       true,
	constants.MediaContentTypeWebP:      true,
	constants.MediaContentTypePDF:       true,
	constants.MediaContentTypePlainText: true,
}

func contentType(v string) (string, error) {
	typ, _, e := mime.ParseMediaType(strings.TrimSpace(v))
	if e != nil || !allowedMIME[strings.ToLower(typ)] {
		return "", bad("content_type must be JPEG, PNG, WebP, PDF or plain text")
	}
	return strings.ToLower(typ), nil
}

func registeredObjectMatches(data map[string]any, info domain.StoredObject) bool {
	ct, err := contentType(info.ContentType)
	if err != nil || ct != platform.Text(data, "content_type") || info.Size != platform.Int(data, "size_bytes") ||
		info.Size > maxUploadSize {
		return false
	}
	// A still-valid upload policy can overwrite an object with the same MIME
	// type and length. Completed uploads also bind their verified content tag.
	etag := platform.Text(data, "etag")
	return etag == "" || etag == info.ETag
}
func (s *Service) Create(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "media.usecase.create")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	name := platform.Text(c.Data, "file_name")
	typ := platform.Text(c.Data, "content_type")
	if typ == "" {
		typ = platform.Text(c.Data, "mime_type")
	}
	ct, e := contentType(typ)
	if e != nil {
		return nil, e
	}
	size := platform.Int(c.Data, "size")
	if size == 0 {
		size = platform.Int(c.Data, "size_bytes")
	}
	if strings.TrimSpace(name) == "" || len(name) > 200 || size < 1 || size > maxUploadSize {
		return nil, bad("file_name and size 1..10MiB required")
	}
	id, e := s.repo.BusinessID(ctx, "file", "", "")
	if e != nil {
		return nil, e
	}
	key := "private/" + id // never derive an object key from user-supplied names
	mode := constants.MediaStorageModeMetadataOnly
	st := constants.MediaStatusRegistered
	data := map[string]any{
		"file_name":        name,
		"content_type":     ct,
		"mime_type":        ct,
		"size_bytes":       size,
		"size":             size,
		"object_key":       key,
		"upload_supported": s.store != nil,
	}
	var signed domain.SignedUpload
	if s.store != nil {
		mode = constants.MediaStorageModeMinio
		st = constants.MediaStatusUploading
		signed, e = s.store.SignUpload(ctx, key, ct, size, signedTTL)
		if e != nil {
			return nil, status.Error(codes.Unavailable, "object upload signing unavailable")
		}
	} else {
		data["message"] = "Configure MINIO_ENDPOINT and credentials for binary upload"
	}
	data["storage_mode"] = mode
	r := record(id, c.UserID, constants.MediaKindMetadata, st, data)
	if e = s.repo.Save(ctx, &r, nil); e != nil {
		return nil, e
	}
	out := view(r)
	out["file_id"] = id
	if s.store != nil {
		out["upload_url"] = signed.URL
		out["upload_method"] = signed.Method
		out["upload_fields"] = signed.Fields
		out["expires_in"] = int64(signedTTL / time.Second)
	}
	return out, nil
}
func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "media.usecase.get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	if c.ID == "" {
		return nil, bad("file ID required")
	}
	r, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if e = own(c, r); e != nil {
		return nil, e
	}
	out := view(r)
	out["file_id"] = r.ID
	if s.store == nil {
		return out, nil
	}
	if r.Status != constants.MediaStatusReady {
		return out, nil
	}

	if r.Status == constants.MediaStatusRejected {
		return nil, status.Error(codes.FailedPrecondition, "upload was rejected")
	}
	data := payload(r)
	key := platform.Text(data, "object_key")
	info, e := s.store.Stat(ctx, key)
	if e != nil {
		return nil, status.Error(
			codes.FailedPrecondition,
			"object not uploaded or storage verification unavailable",
		)
	}
	if !registeredObjectMatches(data, info) {
		if _, saveErr := s.repo.Mutate(
			ctx,
			r,
			false,
			func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
				v.Status = constants.MediaStatusRejected
				return nil, nil, nil
			},
		); saveErr != nil {
			return nil, saveErr
		}
		return nil, status.Error(
			codes.FailedPrecondition,
			"uploaded object content does not match registration",
		)
	}

	download, e := s.store.SignDownload(ctx, key, signedTTL)
	if e != nil {
		return nil, status.Error(codes.Unavailable, "object download signing unavailable")
	}
	out["download_url"] = download
	out["expires_in"] = int64(signedTTL / time.Second)
	return out, nil
}
func (s *Service) Download(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "media.usecase.download")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	if c.ID == "" {
		return nil, bad("file ID required")
	}
	r, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if e = own(c, r); e != nil {
		return nil, e
	}
	out := view(r)
	out["file_id"] = r.ID
	if s.store == nil {
		return nil, status.Error(codes.FailedPrecondition, "object storage is not configured")
	}

	if r.Status != constants.MediaStatusReady {
		return nil, status.Error(codes.FailedPrecondition, "upload is not completed")
	}
	if r.Status == constants.MediaStatusRejected {
		return nil, status.Error(codes.FailedPrecondition, "upload was rejected")
	}
	data := payload(r)
	key := platform.Text(data, "object_key")
	info, e := s.store.Stat(ctx, key)
	if e != nil {
		return nil, status.Error(
			codes.FailedPrecondition,
			"object not uploaded or storage verification unavailable",
		)
	}
	if !registeredObjectMatches(data, info) {
		if _, saveErr := s.repo.Mutate(
			ctx,
			r,
			false,
			func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
				v.Status = constants.MediaStatusRejected
				return nil, nil, nil
			},
		); saveErr != nil {
			return nil, saveErr
		}
		return nil, status.Error(
			codes.FailedPrecondition,
			"uploaded object content does not match registration",
		)
	}

	download, e := s.store.SignDownload(ctx, key, signedTTL)
	if e != nil {
		return nil, status.Error(codes.Unavailable, "object download signing unavailable")
	}
	out["download_url"] = download
	out["expires_in"] = int64(signedTTL / time.Second)
	return out, nil
}
func (s *Service) Complete(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "media.usecase.complete")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}

	if c.ID == "" {
		return nil, bad("file ID required")
	}
	r, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if e = own(c, r); e != nil {
		return nil, e
	}
	out := view(r)
	out["file_id"] = r.ID
	if s.store == nil {
		return nil, status.Error(codes.FailedPrecondition, "object storage is not configured")
	}

	if r.Status == constants.MediaStatusRejected {
		return nil, status.Error(codes.FailedPrecondition, "upload was rejected")
	}
	data := payload(r)
	key := platform.Text(data, "object_key")
	info, e := s.store.Stat(ctx, key)
	if e != nil {
		return nil, status.Error(
			codes.FailedPrecondition,
			"object not uploaded or storage verification unavailable",
		)
	}
	if !registeredObjectMatches(data, info) {
		if _, saveErr := s.repo.Mutate(
			ctx,
			r,
			false,
			func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
				v.Status = constants.MediaStatusRejected
				return nil, nil, nil
			},
		); saveErr != nil {
			return nil, saveErr
		}
		return nil, status.Error(
			codes.FailedPrecondition,
			"uploaded object content does not match registration",
		)
	}
	{
		r, e = s.repo.Mutate(ctx, r, false, func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			if e := own(c, *v); e != nil {
				return nil, nil, e
			}
			if v.Status == constants.MediaStatusRejected {
				return nil, nil, status.Error(codes.FailedPrecondition, "upload was rejected")
			}
			if !registeredObjectMatches(payload(*v), info) {
				return nil, nil, status.Error(codes.FailedPrecondition, "completed upload content changed")
			}
			v.Status = constants.MediaStatusReady
			d := payload(*v)
			d["etag"] = info.ETag
			v.Payload = record(v.ID, v.UserID, v.Kind, v.Status, d).Payload
			return nil, nil, nil
		})
		if e != nil {
			return nil, e
		}
		out = view(r)
		out["file_id"] = r.ID
	}
	download, e := s.store.SignDownload(ctx, key, signedTTL)
	if e != nil {
		return nil, status.Error(codes.Unavailable, "object download signing unavailable")
	}
	out["download_url"] = download
	out["expires_in"] = int64(signedTTL / time.Second)
	return out, nil
}

func (s *Service) seed(context.Context) error                  { return nil }
func (s *Service) event(context.Context, platform.Event) error { return nil }
