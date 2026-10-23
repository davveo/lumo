package media_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/media"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestMinIORealUploadDownload is opt-in. It starts an isolated local MinIO
// process, uploads real bytes through the signed POST policy, and verifies the
// private-object registration/completion/download authorization contract.
//
// MINIO_INTEGRATION_BINARY=/absolute/path/minio go test ./services/media -run TestMinIORealUploadDownload -count=1 -v
func TestMinIORealUploadDownload(t *testing.T) {
	binary := os.Getenv("MINIO_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set MINIO_INTEGRATION_BINARY to run real local S3 integration")
	}
	dataDir := t.TempDir()
	logPath := filepath.Join(dataDir, "minio.log")
	log, e := os.Create(logPath)
	if e != nil {
		t.Fatal(e)
	}
	process := exec.Command(binary, "server", filepath.Join(dataDir, "objects"), "--address", "127.0.0.1:19001", "--console-address", "127.0.0.1:19002")
	process.Env = append(os.Environ(), "MINIO_ROOT_USER=integration-access", "MINIO_ROOT_PASSWORD=integration-secret-32-characters", "MINIO_BROWSER=off")
	process.Stdout = log
	process.Stderr = log
	if e = process.Start(); e != nil {
		_ = log.Close()
		t.Fatal(e)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = process.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = process.Process.Kill()
			<-done
		}
		_ = log.Close()
	}
	t.Cleanup(stop)
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(20 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		r, err := client.Get("http://127.0.0.1:19001/minio/health/live")
		if err == nil {
			_ = r.Body.Close()
			if r.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		stop()
		b, _ := os.ReadFile(logPath)
		t.Fatalf("isolated MinIO did not become ready: %s", b)
	}
	t.Setenv("MINIO_ENDPOINT", "127.0.0.1:19001")
	t.Setenv("MINIO_PUBLIC_ENDPOINT", "127.0.0.1:19001")
	t.Setenv("MINIO_ACCESS_KEY", "integration-access")
	t.Setenv("MINIO_SECRET_KEY", "integration-secret-32-characters")
	t.Setenv("MINIO_BUCKET", fmt.Sprintf("lumo-media-test-%x", time.Now().UnixNano()))
	t.Setenv("MINIO_USE_SSL", "false")
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(dataDir, "media.db"))
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	m := media.New(platform.Dependencies{DB: db})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if e = png.Encode(&encoded, picture); e != nil {
		t.Fatal(e)
	}
	original := encoded.Bytes()
	result, e := m.Handle(ctx, platform.Command{Action: "create", UserID: "demo", Data: map[string]any{"file_name": "receipt.png", "content_type": "image/png", "size": len(original)}})
	if e != nil {
		t.Fatal(e)
	}
	id := platform.Text(result, "file_id")
	if id == "" || platform.Text(result, "upload_method") != "POST" {
		t.Fatalf("invalid upload registration: file=%s method=%s", id, platform.Text(result, "upload_method"))
	}
	var grant struct {
		URL    string            `json:"upload_url"`
		Fields map[string]string `json:"upload_fields"`
	}
	if e = platform.Decode(result, &grant); e != nil {
		t.Fatal(e)
	}
	upload := func(body []byte) int {
		t.Helper()
		var form bytes.Buffer
		writer := multipart.NewWriter(&form)
		for k, v := range grant.Fields {
			if err := writer.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="file"; filename="receipt.png"`)
		header.Set("Content-Type", "image/png")
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(body); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, grant.URL, &form)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if code := upload(original); code != http.StatusNoContent && code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("real policy upload returned HTTP %d", code)
	}
	completed, e := m.Handle(ctx, platform.Command{Action: "complete", ID: id, UserID: "demo"})
	if e != nil {
		t.Fatal(e)
	}
	if platform.Text(completed, "status") != "READY" {
		t.Fatal("Stat-verified upload not READY")
	}
	download := platform.Text(completed, "download_url")
	if !strings.HasPrefix(download, "http://127.0.0.1:19001/") {
		t.Fatal("download URL used a different signing host")
	}
	response, e := client.Get(download)
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if e != nil || response.StatusCode != http.StatusOK || !bytes.Equal(body, original) {
		t.Fatalf("download byte comparison failed: status=%d bytes=%d err=%v", response.StatusCode, len(body), e)
	}
	if _, e = m.Handle(ctx, platform.Command{Action: "download", ID: id, UserID: "other"}); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("cross-user download was not denied: %v", e)
	}
	if code := upload(append(append([]byte(nil), original...), 1)); code != http.StatusBadRequest && code != http.StatusForbidden {
		t.Fatalf("object store accepted a length outside the signed POST policy: %d", code)
	}
	t.Log("Verified real S3 POST upload, Stat completion, signed GET byte equality, cross-user denial and signed-length enforcement")
}
