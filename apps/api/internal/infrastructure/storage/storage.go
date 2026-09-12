// Package storage provides the ObjectStore abstraction over S3-compatible
// object storage (floci/MinIO locally, S3/R2 in production). Two endpoints
// are supported: an internal endpoint for server-side operations (compose
// network) and a public endpoint used when presigning URLs that browsers
// must reach (e.g. http://localhost:4566 on a dev host).
package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config carries connection details for both endpoints.
type Config struct {
	InternalEndpoint string // used by api/worker (compose network)
	PublicEndpoint   string // used inside presigned URLs for browsers
	Region           string
	Bucket           string
	AccessKeyID      string
	SecretAccessKey  string
	UsePathStyle     bool // required for floci/MinIO
	// CDNBaseURL (optional) rewrites playback URLs to CDN/base/key instead
	// of presigned GETs. The CDN pulls from the bucket as origin (public-read
	// policy or origin credentials like S3/R2 OAC).
	CDNBaseURL string
}

// ObjectStore wraps S3 operations with dual-endpoint support.
type ObjectStore struct {
	internal   *s3.Client
	presignPub *s3.PresignClient // presigns against the public endpoint
	bucket     string
	cdnBase    string // optional; empty = always presign
}

// New builds the store and ensures the bucket exists (dev convenience; prod
// infra owns bucket creation via IaC).
func New(ctx context.Context, cfg Config) (*ObjectStore, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	// floci/MinIO can be slow on cold start; give uploads generous timeouts.
	httpClient := awshttp.NewBuildableClient().
		WithTransportOptions(func(t *http.Transport) {
			t.ResponseHeaderTimeout = 60 * time.Second
		})

	newClient := func(endpoint string) *s3.Client {
		return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = cfg.UsePathStyle
			o.HTTPClient = httpClient
		})
	}

	internal := newClient(cfg.InternalEndpoint)
	public := internal
	if cfg.PublicEndpoint != "" && cfg.PublicEndpoint != cfg.InternalEndpoint {
		public = newClient(cfg.PublicEndpoint)
	}

	st := &ObjectStore{
		internal:   internal,
		presignPub: s3.NewPresignClient(public),
		bucket:     cfg.Bucket,
		cdnBase:    cfg.CDNBaseURL,
	}
	if err := st.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *ObjectStore) ensureBucket(ctx context.Context) error {
	_, err := s.internal.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}
	_, err = s.internal.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		// Bucket may exist from a previous run or be owned by floci defaults.
		if _, headErr := s.internal.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)}); headErr == nil {
			return nil
		}
		return fmt.Errorf("create bucket %s: %w", s.bucket, err)
	}
	return nil
}

// PresignedPutURL returns a URL the browser can PUT an object to directly.
func (s *ObjectStore) PresignedPutURL(ctx context.Context, key string, contentType string, ttl time.Duration) (string, error) {
	req, err := s.presignPub.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign put: %w", err)
	}
	return req.URL, nil
}

// PresignedGetURL returns a time-limited playback/download URL. When a CDN
// base URL is configured, it returns CDN/base/key instead — cacheable at the
// edge, no query-string signature (the CDN origin authenticates instead).
func (s *ObjectStore) PresignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if s.cdnBase != "" {
		return s.cdnBase + "/" + key, nil
	}
	req, err := s.presignPub.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign get: %w", err)
	}
	return req.URL, nil
}

// Put streams an object from server-side code (worker outputs, covers).
func (s *ObjectStore) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	_, err := s.internal.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
		Body:        body,
	})
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

// Get opens an object for server-side reads (worker downloads the original).
func (s *ObjectStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.internal.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	return out.Body, nil
}

// Stat returns object size and content type.
func (s *ObjectStore) Stat(ctx context.Context, key string) (int64, string, error) {
	out, err := s.internal.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, "", fmt.Errorf("head %s: %w", key, err)
	}
	size := int64(0)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	ct := ""
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	return size, ct, nil
}

// Delete removes an object (cleanup on failed processing).
func (s *ObjectStore) Delete(ctx context.Context, key string) error {
	_, err := s.internal.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	return err
}
