package media_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/media"
	"github.com/davveo/lumo/services/media/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"image"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"testing"
)

type reviewReadableStore struct {
	*fakeStore
	raw   []byte
	reads int
}

func (s *reviewReadableStore) ReadReviewImage(_ context.Context, key, etag string) ([]byte, error) {
	s.reads++
	if s.objects[key].ETag != etag {
		return nil, errors.New("conditional read changed")
	}
	return s.raw, nil
}
func TestInternalReviewReaderRequiresOwnerAndCompletedBoundObject(t *testing.T) {
	ctx := context.Background()
	var body bytes.Buffer
	_ = png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	store := &reviewReadableStore{fakeStore: &fakeStore{objects: map[string]domain.StoredObject{}}, raw: body.Bytes()}
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "photo.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	m := media.NewWithStore(platform.Dependencies{DB: db}, store)
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	created, e := m.Operations["create"](
		ctx,
		platform.Request{
			UserID: "buyer",
			Role:   "user",
			Data:   map[string]any{"file_name": "photo.png", "content_type": "image/png", "size": body.Len()},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	id := platform.Text(created, "file_id")
	store.objects[store.uploadKey] = domain.StoredObject{
		Size:        int64(body.Len()),
		ContentType: "image/png",
		ETag:        "first-etag",
	}
	if _, e = m.Operations["complete"](ctx, platform.Request{ID: id, UserID: "buyer", Role: "user"}); e != nil {
		t.Fatal(e)
	}
	request := platform.Request{ID: id, UserID: "buyer", Role: "user"}
	if _, e = m.Operations["read_review_image"](ctx, request); status.Code(e) != codes.PermissionDenied {
		t.Fatal("internal reader exposed to ordinary actor", e)
	}
	request.Role = "system"
	request.UserID = "other"
	if _, e = m.Operations["read_review_image"](ctx, request); status.Code(e) != codes.PermissionDenied ||
		store.reads != 0 {
		t.Fatal("foreign private object read", e)
	}
	request.UserID = "buyer"
	result, e := m.Operations["read_review_image"](ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	content, e := base64.StdEncoding.DecodeString(platform.Text(result, "file_content"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = jpeg.DecodeConfig(bytes.NewReader(content)); e != nil {
		t.Fatal("reader did not return sanitized JPEG", e)
	}
	changed := store.objects[store.uploadKey]
	changed.ETag = "overwritten-etag"
	store.objects[store.uploadKey] = changed
	if _, e = m.Operations["read_review_image"](ctx, request); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("overwritten upload escaped approval binding", e)
	}
}
