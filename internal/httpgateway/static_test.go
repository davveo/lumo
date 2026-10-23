package httpgateway

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorefrontStaticFilesAndDebugConsole(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>Storefront</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("window.loaded=true;"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".private"), []byte("must not be served"), 0600); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(nil, false, dir)
	for _, tc := range []struct {
		path string
		code int
		text string
	}{{"/", 200, "Storefront"}, {"/app.js", 200, "window.loaded"}, {"/debug", 200, "LUMO"}, {"/assets/", 404, ""}, {"/.private", 404, ""}, {"/missing.js", 404, ""}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.text) {
			t.Fatalf("%s got%d body%s", tc.path, w.Code, w.Body.String())
		}
	}
}
