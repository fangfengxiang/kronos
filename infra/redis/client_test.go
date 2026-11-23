package redis_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/redis"
)

func TestConfigurationAndExpiryErrors(t *testing.T) {
	for _, config := range []redis.Config{
		{},
		{URL: "https://user:fixture-password@example.com:6379"},
		{URL: "redis://127.0.0.1:6379/1", Cluster: true},
	} {
		client, err := redis.New(config)
		if client != nil || err != redis.ErrInvalidConfig {
			t.Fatalf("invalid configuration returned %v, %v", client, err)
		}
	}
	client, err := redis.New(redis.Config{URL: "redis://127.0.0.1:6379"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Set(t.Context(), "key", "value", -time.Second); !errors.Is(err, redis.ErrInvalidTTL) {
		t.Fatalf("negative TTL = %v", err)
	}
	if _, err := client.SetIfAbsent(t.Context(), "key", "value", time.Microsecond); !errors.Is(err, redis.ErrInvalidTTL) {
		t.Fatalf("sub-millisecond TTL = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := client.Get(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Get = %v", err)
	}
}

func TestGetHonorsDeadlineWithUnresponsiveServer(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	client, err := redis.New(redis.Config{URL: "redis://" + listener.Addr().String() + "?dial_timeout=2s&read_timeout=2s"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, _, err := client.Get(ctx, "key")
		result <- err
	}()
	connection, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unresponsive server returned %v", err)
		}
	case <-time.After(800 * time.Millisecond):
		t.Fatal("Redis request did not return within the caller's deadline")
	}
}

func ExampleNew_invalidConfiguration() {
	_, err := redis.New(redis.Config{})
	fmt.Println(errors.Is(err, redis.ErrInvalidConfig))
	// Output: true
}
