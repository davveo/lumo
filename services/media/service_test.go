package media_test

import (
	"context"
	"errors"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/media"
	"github.com/davveo/lumo/services/media/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	objects               map[string]domain.StoredObject
	uploadKey, uploadType string
	uploadSize            int64
	downloads             int
	ttl                   time.Duration
	ready                 bool
}

func (f *fakeStore) EnsureBucket(context.Context) error { f.ready = true; return nil }
func (f *fakeStore) SignUpload(_ context.Context, key, typ string, size int64, ttl time.Duration) (domain.SignedUpload, error) {
	f.uploadKey = key
	f.uploadType = typ
	f.uploadSize = size
	f.ttl = ttl
	return domain.SignedUpload{URL: "http://localhost:9001/lumo-media", Method: "POST", Fields: map[string]string{"key": key, "Content-Type": typ, "policy": "signed-policy"}}, nil
}
func (f *fakeStore) Stat(_ context.Context, key string) (domain.StoredObject, error) {
	o, ok := f.objects[key]
	if !ok {
		return o, errors.New("not uploaded")
	}
	return o, nil
}
func (f *fakeStore) SignDownload(_ context.Context, key string, ttl time.Duration) (string, error) {
	f.downloads++
	f.ttl = ttl
	return "http://localhost:9001/lumo-media/" + key + "?signature=test", nil
}
func module(t *testing.T, store *fakeStore) platform.Module {
	t.Helper()
	db, e := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "media.db"))
	if e != nil {
		t.Fatal(e)
	}
	m := media.NewWithStore(platform.Dependencies{DB: db}, store)
	if e = m.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	return m
}
func TestMediaUploadCompletionAndOwnership(t *testing.T) {
	f := &fakeStore{objects: map[string]domain.StoredObject{}}
	m := module(t, f)
	ctx := context.Background()
	r, e := m.Handle(ctx, platform.Command{Action: "create", UserID: "demo", Data: map[string]any{"file_name": "../../private.jpg", "content_type": "image/jpeg", "size": 4}})
	if e != nil {
		t.Fatal(e)
	}
	id := platform.Text(r, "file_id")
	if id == "" || platform.Text(r, "upload_method") != "POST" || !f.ready || f.uploadSize != 4 || f.uploadType != "image/jpeg" || f.ttl != 10*time.Minute {
		t.Fatalf("bad upload registration %+v", r)
	}
	if strings.Contains(f.uploadKey, "..") || strings.Contains(f.uploadKey, "private.jpg") || !strings.HasPrefix(f.uploadKey, "private/file_") {
		t.Fatalf("untrusted object key: %s", f.uploadKey)
	}
	if _, e = m.Handle(ctx, platform.Command{Action: "complete", ID: id, UserID: "other"}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("cross-owner complete accepted")
	}
	if _, e = m.Handle(ctx, platform.Command{Action: "complete", ID: id, UserID: "demo"}); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("unuploaded object marked complete")
	}
	f.objects[f.uploadKey] = domain.StoredObject{Size: 4, ContentType: "image/jpeg", ETag: "etag-one"}
	v, e := m.Handle(ctx, platform.Command{Action: "complete", ID: id, UserID: "demo"})
	if e != nil || platform.Text(v, "status") != "READY" || platform.Text(v, "download_url") == "" {
		t.Fatalf("complete: %+v %v", v, e)
	}
	before := f.downloads
	if _, e = m.Handle(ctx, platform.Command{Action: "download", ID: id, UserID: "other"}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("cross-owner download accepted")
	}
	if f.downloads != before {
		t.Fatal("signed URL generated before ownership check")
	}
	if _, e = m.Handle(ctx, platform.Command{Action: "get", ID: id, UserID: "admin", Role: "admin"}); e != nil {
		t.Fatal(e)
	}
	// Every read revalidates the object; an unexpired upload grant cannot bypass
	// size/MIME policy by replacing an already-completed object's contents.
	f.objects[f.uploadKey] = domain.StoredObject{Size: 11 * 1024 * 1024, ContentType: "image/jpeg"}
	if _, e = m.Handle(ctx, platform.Command{Action: "download", ID: id, UserID: "demo"}); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("oversized replacement downloadable")
	}
}
func TestMediaRejectsSizeAndMIME(t *testing.T) {
	m := module(t, &fakeStore{objects: map[string]domain.StoredObject{}})
	for _, d := range []map[string]any{{"file_name": "x.jpg", "content_type": "image/jpeg", "size": 10*1024*1024 + 1}, {"file_name": "x.html", "content_type": "text/html", "size": 1}, {"file_name": "x.pdf", "content_type": "application/pdf", "size": 0}} {
		if _, e := m.Handle(context.Background(), platform.Command{Action: "create", UserID: "demo", Data: d}); status.Code(e) != codes.InvalidArgument {
			t.Fatalf("invalid upload accepted: %+v %v", d, e)
		}
	}
}
func TestMediaWrongObjectMIMECannotComplete(t *testing.T) {
	f := &fakeStore{objects: map[string]domain.StoredObject{}}
	m := module(t, f)
	r, e := m.Handle(context.Background(), platform.Command{Action: "create", UserID: "demo", Data: map[string]any{"file_name": "x.png", "content_type": "image/png", "size": 2}})
	if e != nil {
		t.Fatal(e)
	}
	f.objects[f.uploadKey] = domain.StoredObject{Size: 2, ContentType: "application/pdf"}
	if _, e = m.Handle(context.Background(), platform.Command{Action: "complete", ID: platform.Text(r, "id"), UserID: "demo"}); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("MIME mismatch accepted")
	}
	if f.downloads != 0 {
		t.Fatal("signed download for rejected upload")
	}
}
