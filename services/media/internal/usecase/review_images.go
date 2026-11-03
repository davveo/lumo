package usecase

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
)

type reviewImageReader interface {
	ReadReviewImage(context.Context, string, string) ([]byte, error)
}

var reviewDecodeSlots = make(chan struct{}, 2)

// Re-encoding strips EXIF/location metadata and produces bounded image bytes.
// Decoding has a pixel limit and shared concurrency cap, independent of callers.
func sanitizeReviewImage(ctx context.Context, raw []byte) ([]byte, error) {
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 ||
		cfg.Height > 8192 ||
		int64(cfg.Width)*int64(cfg.Height) > 12000000 {
		return nil, bad("晒图须为有效JPEG或PNG，最多1200万像素")
	}

	src, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return nil, bad("图片内容无法读取")
	}
	width, height := cfg.Width, cfg.Height
	if width > 1600 || height > 1600 {
		if width >= height {
			height = max(1, height*1600/width)
			width = 1600
		} else {
			width = max(1, width*1600/height)
			height = 1600
		}
	}
	resized := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := src.Bounds()
	for y := 0; y < height; y++ {
		if y%128 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		for x := 0; x < width; x++ {
			resized.Set(x, y, src.At(bounds.Min.X+x*cfg.Width/width, bounds.Min.Y+y*cfg.Height/height))
		}
	}
	flat := image.NewRGBA(resized.Bounds())
	draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), resized, image.Point{}, draw.Over)
	var body bytes.Buffer
	if e = jpeg.Encode(&body, flat, &jpeg.Options{Quality: 80}); e != nil {
		return nil, e
	}
	if body.Len() > 2<<20 {
		body.Reset()
		if e = jpeg.Encode(&body, flat, &jpeg.Options{Quality: 60}); e != nil {
			return nil, e
		}
	}
	if body.Len() > 2<<20 {
		return nil, bad("图片过大，请压缩后重新上传")
	}
	return body.Bytes(), nil
}
func (s *Service) ReadReviewImage(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "media.usecase.ReadReviewImage")
	defer sp.End()
	if c.Role != constants.RoleSystem || c.UserID == "" {
		return nil, status.Error(codes.PermissionDenied, "internal image reader required")
	}
	r, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if r.UserID != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "图片不属于该评价作者")
	}
	data := payload(r)
	ct := platform.Text(data, "content_type")
	if r.Status != constants.MediaStatusReady ||
		(ct != constants.MediaContentTypeJPEG && ct != constants.MediaContentTypePNG) ||
		platform.Int(data, "size_bytes") > 2<<20 ||
		platform.Text(data, "etag") == "" {
		return nil, status.Error(codes.FailedPrecondition, "晒图尚未就绪或格式不支持")
	}
	reader, ok := s.store.(reviewImageReader)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "私有图片存储未配置")
	}
	select {
	case reviewDecodeSlots <- struct{}{}:
		defer func() { <-reviewDecodeSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	raw, e := reader.ReadReviewImage(ctx, platform.Text(data, "object_key"), platform.Text(data, "etag"))
	if e != nil {
		return nil, status.Error(codes.FailedPrecondition, "图片未上传完成或已发生变化")
	}
	if len(raw) > 2<<20 {
		return nil, bad("晒图超过2MiB")
	}
	content, e := sanitizeReviewImage(ctx, raw)
	if e != nil {
		return nil, e
	}
	return platform.Result{
		"file_content": base64.StdEncoding.EncodeToString(content),
		"content_type": constants.MediaContentTypeJPEG,
		"size_bytes":   len(content),
	}, nil
}
