package storage

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/media/internal/domain"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint, PublicEndpoint, AccessKey, SecretKey, Bucket string
	UseSSL                                                 bool
}
type MinIO struct {
	internal, public *minio.Client
	bucket           string
}

func FromEnv() (domain.ObjectStore, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		return nil, nil
	}
	return New(
		Config{
			Endpoint:       endpoint,
			PublicEndpoint: os.Getenv("MINIO_PUBLIC_ENDPOINT"),
			AccessKey:      os.Getenv("MINIO_ACCESS_KEY"),
			SecretKey:      os.Getenv("MINIO_SECRET_KEY"),
			Bucket:         os.Getenv("MINIO_BUCKET"),
			UseSSL:         os.Getenv("MINIO_USE_SSL") == "true",
		},
	)
}
func endpoint(raw string, secure bool) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, fmt.Errorf("MinIO endpoint is required")
	}
	if strings.Contains(raw, "://") {
		u, e := url.Parse(raw)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
			u.Path != "" && u.Path != "/" ||
			u.Scheme != "http" && u.Scheme != "https" {
			return "", false, fmt.Errorf("invalid MinIO endpoint")
		}
		return u.Host, u.Scheme == "https", nil
	}
	if strings.ContainsAny(raw, "/?#@") {
		return "", false, fmt.Errorf("invalid MinIO endpoint")
	}
	return raw, secure, nil
}
func New(c Config) (*MinIO, error) {
	if c.AccessKey == "" || c.SecretKey == "" {
		return nil, fmt.Errorf("MinIO access key and secret key are required")
	}
	if c.Bucket == "" {
		c.Bucket = "lumo-media"
	}
	internal, internalSSL, e := endpoint(c.Endpoint, c.UseSSL)
	if e != nil {
		return nil, e
	}
	public, publicSSL, e := endpoint(c.PublicEndpoint, c.UseSSL)
	if e != nil {
		return nil, e
	}
	opts := func(ssl bool) *minio.Options {
		return &minio.Options{
			Creds:        credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
			Secure:       ssl,
			Region:       "us-east-1",
			BucketLookup: minio.BucketLookupPath,
		}
	}
	ic, e := minio.New(internal, opts(internalSSL))
	if e != nil {
		return nil, e
	}
	pc, e := minio.New(public, opts(publicSSL))
	if e != nil {
		return nil, e
	}
	return &MinIO{internal: ic, public: pc, bucket: c.Bucket}, nil
}
func (s *MinIO) EnsureBucket(ctx context.Context) (spanErr0 error) {
	ctx, span := platform.Start(ctx, "media.storage.EnsureBucket")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	exists, e := s.internal.BucketExists(ctx, s.bucket)
	if e != nil {
		return fmt.Errorf("check private media bucket: %w", e)
	}
	if !exists {
		e = s.internal.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: "us-east-1"})
		if e != nil && minio.ToErrorResponse(e).Code != "BucketAlreadyOwnedByYou" {
			return fmt.Errorf("create private media bucket: %w", e)
		}
	}
	policy, e := s.internal.GetBucketPolicy(ctx, s.bucket)
	if e != nil && minio.ToErrorResponse(e).Code != "NoSuchBucketPolicy" {
		return fmt.Errorf("check media bucket policy: %w", e)
	}
	if strings.TrimSpace(policy) != "" {
		return fmt.Errorf("media bucket must have no public bucket policy")
	}
	return nil
}

func (s *MinIO) SignUpload(
	ctx context.Context,
	key, contentType string,
	size int64,
	ttl time.Duration,
) (spanResult0 domain.SignedUpload, spanErr0 error) {
	ctx, span := platform.Start(ctx, "media.storage.SignUpload")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	p := minio.NewPostPolicy()
	for _, e := range []error{
		p.SetBucket(s.bucket),
		p.SetKey(key),
		p.SetExpires(time.Now().UTC().Add(ttl)),
		p.SetContentType(contentType),
		p.SetContentLengthRange(size, size),
	} {
		if e != nil {
			return domain.SignedUpload{}, e
		}
	}
	u, fields, e := s.public.PresignedPostPolicy(ctx, p)
	if e != nil {
		return domain.SignedUpload{}, e
	}
	return domain.SignedUpload{URL: u.String(), Method: "POST", Fields: fields}, nil
}
func (s *MinIO) Stat(ctx context.Context, key string) (spanResult0 domain.StoredObject, spanErr0 error) {
	ctx, span := platform.Start(ctx, "media.storage.StatObject")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	v, e := s.internal.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if e != nil {
		return domain.StoredObject{}, e
	}
	return domain.StoredObject{Size: v.Size, ContentType: v.ContentType, ETag: v.ETag}, nil
}
func (s *MinIO) SignDownload(ctx context.Context, key string, ttl time.Duration) (spanResult0 string, spanErr0 error) {
	ctx, span := platform.Start(ctx, "media.storage.SignDownload")
	defer span.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	u, e := s.public.PresignedGetObject(
		ctx,
		s.bucket,
		key,
		ttl,
		url.Values{"response-content-disposition": []string{"attachment"}},
	)
	if e != nil {
		return "", e
	}
	return u.String(), nil
}
