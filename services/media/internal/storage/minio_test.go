package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignUsesPublicEndpointAndBoundPOSTPolicy(t *testing.T) {
	s, e := New(Config{Endpoint: "minio:9000", PublicEndpoint: "localhost:9001", AccessKey: "development-access", SecretKey: "development-secret-32-characters", Bucket: "lumo-media"})
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.SignUpload(context.Background(), "private/file_random", "image/png", 123, 10*time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := url.Parse(u.URL)
	if e != nil || parsed.Host != "localhost:9001" || !strings.HasPrefix(parsed.Path, "/lumo-media") {
		t.Fatalf("wrong signing endpoint %s %v", u.URL, e)
	}
	if u.Fields["key"] != "private/file_random" || u.Fields["Content-Type"] != "image/png" || u.Fields["x-amz-signature"] == "" {
		t.Fatalf("incomplete signed form %+v", u.Fields)
	}
	b, e := base64.StdEncoding.DecodeString(u.Fields["policy"])
	if e != nil {
		t.Fatal(e)
	}
	var p struct {
		Conditions []any `json:"conditions"`
	}
	if e = json.Unmarshal(b, &p); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, c := range p.Conditions {
		if v, ok := c.([]any); ok && len(v) == 3 && v[0] == "content-length-range" {
			found = v[1] == float64(123) && v[2] == float64(123)
		}
	}
	if !found {
		t.Fatalf("no exact size constraint: %s", b)
	}
	d, e := s.SignDownload(context.Background(), "private/file_random", 10*time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	parsed, e = url.Parse(d)
	if e != nil || parsed.Host != "localhost:9001" || parsed.Query().Get("X-Amz-Signature") == "" {
		t.Fatalf("bad download signing endpoint %s %v", d, e)
	}
}
func TestRejectsUnsafeEndpoint(t *testing.T) {
	for _, raw := range []string{"http://user:password@localhost:9000", "http://localhost:9000/path", "localhost:9000?secret=x"} {
		if _, _, e := endpoint(raw, false); e == nil {
			t.Fatalf("unsafe endpoint accepted %q", raw)
		}
	}
}
