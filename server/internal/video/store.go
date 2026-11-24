package video

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// object is one listed key.
type object struct {
	key          string
	size         int64
	lastModified time.Time
}

// errObjectUnreadable means a single object is missing or forbidden, as opposed
// to the store itself failing.
var errObjectUnreadable = errors.New("object is missing or forbidden")

// objectStore is everything this package may do with the bucket. The key it
// runs with can also write, so nothing beyond list, stat and presigned GET is
// reachable from here.
type objectStore interface {
	list(ctx context.Context, prefix string) ([]object, error)
	// stat returns the stored Content-Type, or errObjectUnreadable.
	stat(ctx context.Context, key string) (contentType string, err error)
	presignGet(ctx context.Context, key string, expires time.Duration, disposition string) (string, error)
	publicURL(key string) string
}

type s3Store struct {
	client   *minio.Client
	endpoint *url.URL
	bucket   string
}

// newS3Store builds the client without contacting the store. Its errors are
// safe to log: they name settings, never values.
func newS3Store(opts Options) (*s3Store, error) {
	switch {
	case opts.Bucket == "":
		return nil, errors.New("VIDEO_S3_BUCKET is unset")
	case opts.AccessKey == "" || opts.SecretKey == "":
		return nil, errors.New("VIDEO_S3_ACCESS_KEY or VIDEO_S3_SECRET_KEY is unset")
	}
	endpoint, err := url.Parse(opts.Endpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.Host == "" || endpoint.User != nil {
		return nil, errors.New("VIDEO_S3_ENDPOINT is not an http(s) URL")
	}
	client, err := minio.New(endpoint.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
		Secure: endpoint.Scheme == "https",
		// Explicit, so the client never asks for the bucket's location.
		Region:       opts.Region,
		BucketLookup: minio.BucketLookupAuto,
		// An outage should fail fast; the next request after the failure window
		// is the retry.
		MaxRetries: 1,
	})
	if err != nil {
		return nil, errors.New("VIDEO_S3_ENDPOINT is not a valid S3 endpoint")
	}
	return &s3Store{client: client, endpoint: endpoint, bucket: opts.Bucket}, nil
}

func (s *s3Store) list(ctx context.Context, prefix string) ([]object, error) {
	// The listing goroutine stops only when its context ends.
	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var out []object
	for info := range s.client.ListObjects(listCtx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if info.Err != nil {
			return nil, fmt.Errorf("list videos: %w", safeError(info.Err))
		}
		out = append(out, object{key: info.Key, size: info.Size, lastModified: info.LastModified})
	}
	// When the context ends between pages the client closes the channel without
	// an error, which would otherwise pass for a complete listing.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list videos: %w", err)
	}
	return out, nil
}

func (s *s3Store) stat(ctx context.Context, key string) (string, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		switch minio.ToErrorResponse(err).StatusCode {
		case http.StatusForbidden, http.StatusNotFound:
			return "", errObjectUnreadable
		}
		return "", fmt.Errorf("stat %s: %w", key, safeError(err))
	}
	return info.ContentType, nil
}

func (s *s3Store) presignGet(ctx context.Context, key string, expires time.Duration, disposition string) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expires,
		url.Values{"response-content-disposition": {disposition}})
	if err != nil {
		return "", fmt.Errorf("presign %s: %w", key, safeError(err))
	}
	return u.String(), nil
}

// publicURL is the virtual-hosted address of a public-read object.
func (s *s3Store) publicURL(key string) string {
	segments := strings.Split(key, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return s.endpoint.Scheme + "://" + s.bucket + "." + s.endpoint.Host + "/" + strings.Join(segments, "/")
}

// safeError reduces a store response to its code and status. Response bodies
// can echo request details, and these errors are logged.
func safeError(err error) error {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) {
		return fmt.Errorf("store responded %s (status %d)", resp.Code, resp.StatusCode)
	}
	return err
}
