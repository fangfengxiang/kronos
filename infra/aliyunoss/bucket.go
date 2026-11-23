// Package aliyunoss provides access to objects in an Alibaba Cloud OSS bucket.
package aliyunoss

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/transport"
)

var (
	// ErrInvalidConfig indicates an invalid region, bucket, endpoint, or credentials configuration.
	ErrInvalidConfig = errors.New("invalid OSS configuration")
	// ErrInvalidObject indicates an invalid key or a missing upload reader.
	ErrInvalidObject = errors.New("invalid OSS object")
	// ErrInvalidExpiry indicates a duration outside the supported V4 presign range.
	ErrInvalidExpiry = errors.New("invalid OSS download URL expiry")
	// ErrNotFound indicates that the object does not exist in this bucket.
	ErrNotFound = errors.New("OSS object not found")
	// ErrPermissionDenied indicates that OSS rejected the credentials or operation.
	ErrPermissionDenied = errors.New("OSS permission denied")
	// ErrUnavailable indicates that OSS did not complete an operation successfully.
	ErrUnavailable = errors.New("OSS operation unavailable")
)

const maxPresignDuration = 7 * 24 * time.Hour

var endpointLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Config identifies a bucket and its access credentials. An empty Endpoint
// uses the regional HTTPS endpoint. UseCNAME requires a bucket-bound domain.
type Config struct {
	Region          string
	Bucket          string
	Endpoint        string
	UseCNAME        bool
	AccessKeyID     string
	AccessKeySecret string
	SessionToken    string
}

// Bucket owns an independent HTTP client. It supports concurrent operations.
type Bucket struct {
	name       string
	client     *oss.Client
	httpClient *http.Client
}

// New configures a bucket client. Its first operation verifies remote access.
func New(config Config) (*Bucket, error) {
	if !endpointLabel.MatchString(config.Region) || len(config.Bucket) < 3 || len(config.Bucket) > 63 || !endpointLabel.MatchString(config.Bucket) ||
		strings.TrimSpace(config.AccessKeyID) == "" || strings.TrimSpace(config.AccessKeySecret) == "" {
		return nil, ErrInvalidConfig
	}
	if config.Endpoint != "" || config.UseCNAME {
		endpoint, err := url.Parse(config.Endpoint)
		if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") ||
			endpoint.User != nil || endpoint.ForceQuery || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
			(endpoint.Path != "" && endpoint.Path != "/") || (config.UseCNAME && net.ParseIP(endpoint.Hostname()) != nil) {
			return nil, ErrInvalidConfig
		}
	}
	httpClient := transport.NewHttpClient(nil)
	sdkConfig := oss.LoadDefaultConfig().
		WithRegion(config.Region).
		WithEndpoint(config.Endpoint).
		WithUseCName(config.UseCNAME).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.AccessKeySecret, config.SessionToken)).
		WithHttpClient(httpClient).
		WithLogLevel(oss.LogOff)
	client := oss.NewClient(sdkConfig, func(options *oss.Options) {
		// The SDK mutates its shared clock offset without synchronization.
		options.FeatureFlags &^= oss.FeatureCorrectClockSkew
	})
	return &Bucket{name: config.Bucket, client: client, httpClient: httpClient}, nil
}

// CloseIdleConnections releases pooled connections after the caller drains its work.
func (bucket *Bucket) CloseIdleConnections() {
	bucket.httpClient.CloseIdleConnections()
}

// Open starts a streaming download. The caller closes the body and keeps ctx
// alive until the download finishes.
func (bucket *Bucket) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateObjectKey(ctx, key); err != nil {
		return nil, err
	}
	result, err := bucket.client.GetObject(ctx, &oss.GetObjectRequest{Bucket: &bucket.name, Key: &key})
	if err != nil {
		return nil, operationError(ctx, err)
	}
	return result.Body, nil
}

// Put uploads from the reader's current position. The caller owns the reader.
func (bucket *Bucket) Put(ctx context.Context, key string, content io.ReadSeeker) error {
	if err := validateObjectKey(ctx, key); err != nil {
		return err
	}
	if content == nil {
		return ErrInvalidObject
	}
	_, err := bucket.client.PutObject(ctx, &oss.PutObjectRequest{Bucket: &bucket.name, Key: &key, Body: content})
	return operationError(ctx, err)
}

// PresignGet authorizes a time-limited GET for key. The expiry is specified in
// whole seconds up to seven days and is also subject to credential expiry.
func (bucket *Bucket) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := validateObjectKey(ctx, key); err != nil {
		return "", err
	}
	if ttl < time.Second || ttl > maxPresignDuration || ttl%time.Second != 0 {
		return "", ErrInvalidExpiry
	}
	signed, err := bucket.client.Presign(ctx, &oss.GetObjectRequest{Bucket: &bucket.name, Key: &key}, oss.PresignExpires(ttl))
	if err != nil {
		return "", operationError(ctx, err)
	}
	if !signed.Expiration.Truncate(time.Second).After(time.Now()) {
		return "", ErrUnavailable
	}
	return signed.URL, nil
}

func validateObjectKey(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(key) == 0 || len(key) > 1023 || key[0] == '/' || key[0] == '\\' || !utf8.ValidString(key) {
		return ErrInvalidObject
	}
	return nil
}

func operationError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if contextError := ctx.Err(); contextError != nil {
		return contextError
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var serviceError *oss.ServiceError
	if errors.As(err, &serviceError) {
		switch {
		case serviceError.StatusCode == http.StatusNotFound && serviceError.Code == "NoSuchKey":
			return ErrNotFound
		case serviceError.StatusCode == http.StatusUnauthorized || serviceError.StatusCode == http.StatusForbidden:
			return ErrPermissionDenied
		}
	}
	return ErrUnavailable
}
