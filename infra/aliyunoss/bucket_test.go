package aliyunoss_test

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/fangfengxiang/kronos/infra/aliyunoss"
)

func TestBucketMatchesNativeSDK(t *testing.T) {
	var objects sync.Map
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("x-oss-signature") == "" {
			if !strings.HasPrefix(request.Header.Get("Authorization"), "OSS4-HMAC-SHA256 Credential=fixture-key/") || request.Header.Get("x-oss-security-token") != "fixture-session" {
				writeServiceError(writer, http.StatusForbidden, "AccessDenied")
				return
			}
		} else if request.Method != http.MethodGet || query.Get("x-oss-security-token") != "fixture-session" {
			writeServiceError(writer, http.StatusForbidden, "AccessDenied")
			return
		}
		switch request.Method {
		case http.MethodPut:
			content, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			objects.Store(request.URL.Path, string(content))
		case http.MethodGet:
			content, found := objects.Load(request.URL.Path)
			if !found {
				writeServiceError(writer, http.StatusNotFound, "NoSuchKey")
				return
			}
			io.WriteString(writer, content.(string))
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	config := fixtureConfig(server.URL)
	bucket := newFixtureBucket(t, config)
	native := oss.NewClient(oss.LoadDefaultConfig().
		WithRegion(config.Region).WithEndpoint(config.Endpoint).WithHttpClient(server.Client()).WithLogLevel(oss.LogOff).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.AccessKeySecret, config.SessionToken)))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	const key = "profiles/你好 +#?.txt"
	const prefix = "欢迎："
	upload := strings.NewReader("skip:" + prefix)
	if _, err := upload.Seek(int64(len("skip:")), io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := bucket.Put(ctx, key, upload); err != nil {
		t.Fatal(err)
	}
	request := &oss.GetObjectRequest{Bucket: &config.Bucket, Key: oss.Ptr(key)}
	nativeObject, err := native.GetObject(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if got := readContent(t, nativeObject.Body); got != prefix {
		t.Fatalf("native read = %q, want %q", got, prefix)
	}
	if actual, found := objects.Load("/" + config.Bucket + "/" + key); !found || actual != prefix {
		t.Fatalf("stored object = %q, present = %v", actual, found)
	}
	const updated = "новый · "
	if _, err := native.PutObject(ctx, &oss.PutObjectRequest{Bucket: &config.Bucket, Key: oss.Ptr(key), Body: strings.NewReader(updated)}); err != nil {
		t.Fatal(err)
	}
	body, err := bucket.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if got := readContent(t, body); got != updated {
		t.Fatalf("Kronos read = %q, want %q", got, updated)
	}
	const ttl = 2 * time.Minute
	signedURL, err := bucket.PresignGet(ctx, key, ttl)
	if err != nil {
		t.Fatal(err)
	}
	nativeSignature, err := native.Presign(ctx, request, oss.PresignExpires(ttl))
	if err != nil {
		t.Fatal(err)
	}
	for _, signed := range []string{signedURL, nativeSignature.URL} {
		validateDownloadURL(t, signed, config.Bucket, key, ttl)
		download, err := http.NewRequestWithContext(ctx, http.MethodGet, signed, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Do(download)
		if err != nil {
			t.Fatal(err)
		}
		content := readContent(t, response.Body)
		if response.StatusCode != http.StatusOK || content != updated {
			t.Fatalf("signed download = %d, %q", response.StatusCode, content)
		}
	}
	otherConfig := config
	otherConfig.Bucket = "fixture-other"
	other := newFixtureBucket(t, otherConfig)
	if body, err := other.Open(ctx, key); body != nil || !errors.Is(err, aliyunoss.ErrNotFound) {
		t.Fatalf("independent bucket read = %v, %v", body, err)
	}
	if err := other.Put(ctx, key, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	body, err = other.Open(ctx, key)
	if err != nil || readContent(t, body) != "" {
		t.Fatalf("read empty object returned %v", err)
	}
}

func TestBucketErrorsAndBodyCancellation(t *testing.T) {
	streamCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/fixture-bucket/missing":
			writeServiceError(writer, http.StatusNotFound, "NoSuchKey")
		case "/fixture-bucket/missing-bucket":
			writeServiceError(writer, http.StatusNotFound, "NoSuchBucket")
		case "/fixture-bucket/private":
			writeServiceError(writer, http.StatusForbidden, "AccessDenied")
		case "/fixture-bucket/stream":
			io.WriteString(writer, "a")
			if err := http.NewResponseController(writer).Flush(); err != nil {
				t.Error(err)
				return
			}
			<-request.Context().Done()
			close(streamCanceled)
		default:
			writer.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	bucket := newFixtureBucket(t, fixtureConfig(server.URL))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for key, expected := range map[string]error{"missing": aliyunoss.ErrNotFound, "missing-bucket": aliyunoss.ErrUnavailable, "private": aliyunoss.ErrPermissionDenied} {
		if body, err := bucket.Open(ctx, key); body != nil || !errors.Is(err, expected) {
			t.Fatalf("Open(%q) = %v, %v, want %v", key, body, err, expected)
		}
	}
	callContext, cancelCall := context.WithCancel(ctx)
	defer cancelCall()
	body, err := bucket.Open(callContext, "stream")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	first := make([]byte, 1)
	if _, err := io.ReadFull(body, first); err != nil || string(first) != "a" {
		t.Fatalf("initial stream data = %q, %v", first, err)
	}
	cancelCall()
	if _, err := io.ReadAll(body); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled body read = %v", err)
	}
	select {
	case <-streamCanceled:
	case <-ctx.Done():
		t.Fatal("HTTP server did not observe cancellation")
	}
	if _, err := bucket.PresignGet(callContext, "stream", time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled presign = %v", err)
	}
}

func TestBucketContinuesUsingSystemClockAfterTimeSkewResponse(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		date, err := time.Parse("20060102T150405Z", request.Header.Get("x-oss-date"))
		if err != nil || time.Until(date).Abs() > 15*time.Second {
			t.Errorf("request used a date outside the local clock: %q, %v", request.Header.Get("x-oss-date"), err)
		}
		if requests.Add(1) == 1 {
			writer.Header().Set("Date", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
			writeServiceError(writer, http.StatusForbidden, "RequestTimeTooSkewed")
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	bucket := newFixtureBucket(t, fixtureConfig(server.URL))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var concurrent sync.WaitGroup
	for range 6 {
		concurrent.Go(func() {
			body, err := bucket.Open(ctx, "clock")
			if err != nil {
				t.Error(err)
				return
			}
			_, readError := io.Copy(io.Discard, body)
			if err := errors.Join(readError, body.Close()); err != nil {
				t.Error(err)
			}
		})
	}
	concurrent.Wait()
	if count := requests.Load(); count != 7 {
		t.Fatalf("request count = %d, want 6 initial requests plus one SDK retry", count)
	}
}

func TestBucketValidatesConfigurationAndParameters(t *testing.T) {
	for _, endpoint := range []string{"https://user:secret@example.com", "https://example.com/path", "https://example.com?key=token", "file:///tmp/oss", "https://"} {
		if bucket, err := aliyunoss.New(fixtureConfig(endpoint)); bucket != nil || err != aliyunoss.ErrInvalidConfig {
			t.Fatalf("invalid endpoint produced %v, %v", bucket, err)
		}
	}
	config := fixtureConfig("https://files.example.com")
	config.UseCNAME = true
	bucket := newFixtureBucket(t, config)
	ctx := t.Context()
	for _, key := range []string{"", "/leading", `\leading`, strings.Repeat("中", 342), string([]byte{0xff})} {
		if _, err := bucket.PresignGet(ctx, key, time.Minute); err != aliyunoss.ErrInvalidObject {
			t.Fatalf("invalid key produced %v", err)
		}
	}
	for _, duration := range []time.Duration{0, 999 * time.Millisecond, time.Second + time.Millisecond, 7*24*time.Hour + time.Second} {
		if _, err := bucket.PresignGet(ctx, "greeting", duration); err != aliyunoss.ErrInvalidExpiry {
			t.Fatalf("invalid TTL %v produced %v", duration, err)
		}
	}
	if err := bucket.Put(ctx, "greeting", nil); err != aliyunoss.ErrInvalidObject {
		t.Fatalf("nil reader produced %v", err)
	}
	signed, err := bucket.PresignGet(ctx, "greeting", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(signed)
	if err != nil || parsed.Hostname() != "files.example.com" || parsed.Path != "/greeting" {
		t.Fatalf("unexpected bucket-bound endpoint: %v", err)
	}
}

func fixtureConfig(endpoint string) aliyunoss.Config {
	return aliyunoss.Config{Region: "cn-hangzhou", Bucket: "fixture-bucket", Endpoint: endpoint,
		AccessKeyID: "fixture-key", AccessKeySecret: "fixture-secret", SessionToken: "fixture-session"}
}

func newFixtureBucket(t *testing.T, config aliyunoss.Config) *aliyunoss.Bucket {
	t.Helper()
	bucket, err := aliyunoss.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bucket.CloseIdleConnections)
	return bucket
}

func validateDownloadURL(t *testing.T, signed, bucket, key string, ttl time.Duration) {
	t.Helper()
	parsed, err := url.Parse(signed)
	if err != nil || parsed.Path != "/"+bucket+"/"+key {
		t.Fatalf("presigned download did not retain the bucket and key: %v", err)
	}
	query := parsed.Query()
	expires, expiryError := strconv.Atoi(query.Get("x-oss-expires"))
	signature, signatureError := hex.DecodeString(query.Get("x-oss-signature"))
	if expiryError != nil || expires < int(ttl.Seconds())-1 || expires > int(ttl.Seconds()) || signatureError != nil || len(signature) != 32 ||
		query.Get("x-oss-signature-version") != "OSS4-HMAC-SHA256" || !strings.Contains(query.Get("x-oss-credential"), "/cn-hangzhou/oss/") {
		t.Fatal("presigned download did not carry the expected V4 scope, signature and expiry")
	}
}

func writeServiceError(writer http.ResponseWriter, status int, code string) {
	writer.Header().Set("Content-Type", "application/xml")
	writer.WriteHeader(status)
	fmt.Fprintf(writer, "<Error><Code>%s</Code><Message>fixture diagnostic</Message></Error>", code)
}

func readContent(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	defer body.Close()
	content, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func ExampleBucket_PresignGet() {
	bucket, err := aliyunoss.New(aliyunoss.Config{
		Region: "cn-hangzhou", Bucket: "example-bucket",
		AccessKeyID: "example-key", AccessKeySecret: "example-secret",
	})
	if err != nil {
		panic(err)
	}
	defer bucket.CloseIdleConnections()
	signed, err := bucket.PresignGet(context.Background(), "profiles/greeting.txt", 5*time.Minute)
	if err != nil {
		panic(err)
	}
	parsed, err := url.Parse(signed)
	if err != nil {
		panic(err)
	}
	fmt.Println(parsed.Scheme, parsed.Hostname(), parsed.Path)
	// Output: https example-bucket.oss-cn-hangzhou.aliyuncs.com /profiles/greeting.txt
}
