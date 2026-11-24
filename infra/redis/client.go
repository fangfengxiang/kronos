// Package redis implements a string cache on Redis and Redis Cluster.
package redis

import (
	"context"
	"errors"
	"net/url"
	"os"
	"time"

	redisdriver "github.com/redis/go-redis/v9"
)

var (
	// ErrInvalidConfig indicates that the URL is invalid for the selected mode.
	ErrInvalidConfig = errors.New("invalid Redis configuration")
	// ErrInvalidTTL indicates that a TTL cannot be represented by Redis.
	ErrInvalidTTL = errors.New("Redis TTL must be zero or at least one millisecond")
	// ErrClosed indicates that a command was attempted after closing the client.
	ErrClosed = errors.New("Redis client is closed")
	// ErrUnavailable indicates that Redis did not complete a command successfully.
	ErrUnavailable = errors.New("Redis command unavailable")
)

// Config selects an endpoint and deployment mode. Cluster URLs can supply
// additional seed addresses with repeated addr query parameters.
type Config struct {
	URL     string
	Cluster bool
}

// Client owns the connections used by a string cache.
type Client struct {
	native redisdriver.UniversalClient
}

// New configures a client and opens connections when commands need them.
func New(config Config) (*Client, error) {
	if config.Cluster {
		endpoint, err := url.Parse(config.URL)
		if err != nil || (endpoint.Path != "" && endpoint.Path != "/" && endpoint.Path != "/0") {
			return nil, ErrInvalidConfig
		}
		options, err := redisdriver.ParseClusterURL(config.URL)
		if err != nil {
			return nil, ErrInvalidConfig
		}
		options.ContextTimeoutEnabled = true
		options.MaxRetries = -1
		options.DialerRetries = 1
		return &Client{native: redisdriver.NewClusterClient(options)}, nil
	}
	options, err := redisdriver.ParseURL(config.URL)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	options.ContextTimeoutEnabled = true
	options.MaxRetries = -1
	options.DialerRetries = 1
	return &Client{native: redisdriver.NewClient(options)}, nil
}

// Get distinguishes a missing key from an existing key with an empty value.
func (client *Client) Get(ctx context.Context, key string) (value string, found bool, err error) {
	value, err = client.native.Get(ctx, key).Result()
	if errors.Is(err, redisdriver.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, commandError(ctx, err)
	}
	return value, true, nil
}

// Set stores value with the given TTL. Zero creates a persistent value.
func (client *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl != 0 && ttl < time.Millisecond {
		return ErrInvalidTTL
	}
	return commandError(ctx, client.native.Set(ctx, key, value, ttl).Err())
}

// SetIfAbsent sets the value and TTL atomically if the key is absent.
func (client *Client) SetIfAbsent(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if ttl != 0 && ttl < time.Millisecond {
		return false, ErrInvalidTTL
	}
	stored, err := client.native.SetNX(ctx, key, value, ttl).Result()
	return stored, commandError(ctx, err)
}

// Delete removes the key if it exists.
func (client *Client) Delete(ctx context.Context, key string) error {
	return commandError(ctx, client.native.Del(ctx, key).Err())
}

// Close releases the connections. Closing again has no effect.
func (client *Client) Close() error {
	if err := client.native.Close(); err != nil && !errors.Is(err, redisdriver.ErrClosed) {
		return ErrUnavailable
	}
	return nil
}

func commandError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if contextError := ctx.Err(); contextError != nil {
		return contextError
	}
	// The socket can reach the caller's deadline before the context timer runs.
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline && errors.Is(err, os.ErrDeadlineExceeded) && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, redisdriver.ErrClosed) {
		return ErrClosed
	}
	return ErrUnavailable
}
