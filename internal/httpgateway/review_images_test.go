package httpgateway

import (
	"context"
	"encoding/base64"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
	"net/http/httptest"
	"testing"
)

type reviewImagePeer struct{ withdrawn bool }

func (p *reviewImagePeer) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	if service == "interaction" && c.Action == "review_image" {
		if p.withdrawn {
			return nil, status.Error(codes.NotFound, "图片已撤回")
		}
		return platform.Result{
			"content_type": "image/jpeg",
			"file_content": base64.StdEncoding.EncodeToString([]byte{255, 216, 255, 217}),
		}, nil
	}
	return nil, status.Error(codes.Unimplemented, "unexpected image request")
}
func TestReviewImageGETIsBinaryAndCannotCacheWithdrawnContent(t *testing.T) {
	peer := &reviewImagePeer{}
	handler := NewHandler(peer, false, "")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/interaction/review_image?id=review&file_id=photo", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || response.Header().Get("Content-Type") != "image/jpeg" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Body.Len() != 4 {
		t.Fatal(response.Code, response.Header(), response.Body.String())
	}
	peer.withdrawn = true
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 404 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("withdrawal was not rechecked", response.Code, response.Header())
	}
}
