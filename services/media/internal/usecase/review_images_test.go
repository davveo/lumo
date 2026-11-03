package usecase

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestReviewImagesReencodeAndBoundDimensions(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1601, 2))
	src.Set(1, 1, color.RGBA{R: 255, A: 255})
	var raw bytes.Buffer
	if e := png.Encode(&raw, src); e != nil {
		t.Fatal(e)
	}
	raw.WriteString("private-location-metadata")
	out, e := sanitizeReviewImage(context.Background(), raw.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := jpeg.DecodeConfig(bytes.NewReader(out))
	if e != nil || cfg.Width != 1600 || bytes.Contains(out, []byte("private-location-metadata")) {
		t.Fatal("image did not pass through bounded metadata-stripping encoding", cfg, e)
	}
	if _, e = sanitizeReviewImage(context.Background(), []byte(`<svg><script>alert(1)</script></svg>`)); e == nil {
		t.Fatal("active image content accepted")
	}
}
