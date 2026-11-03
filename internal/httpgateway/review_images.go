package httpgateway

import (
	"context"
	"encoding/base64"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
	"strconv"
)

func writeReviewImage(w http.ResponseWriter, ctx context.Context, result platform.Result) {
	content, e := base64.StdEncoding.DecodeString(platform.Text(result, "file_content"))
	if e != nil || len(content) == 0 || len(content) > 2<<20 || platform.Text(result, "content_type") != "image/jpeg" {
		writeError(w, ctx, status.Error(codes.Internal, "图片响应无效"))
		return
	}
	traceHTTPResponse(ctx, platform.Result{"content_type": "image/jpeg", "size_bytes": len(content)}, nil)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
