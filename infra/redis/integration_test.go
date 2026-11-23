//go:build redis_integration

package redis_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/redis"
	redisdriver "github.com/redis/go-redis/v9"
)

func TestStringCacheMatchesNativeRedis(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		variable string
		cluster  bool
	}{
		{name: "standalone", variable: "KRONOS_REDIS_TEST_URL"},
		{name: "cluster", variable: "KRONOS_REDIS_CLUSTER_TEST_URL", cluster: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			endpoint := os.Getenv(testCase.variable)
			if endpoint == "" {
				t.Fatalf("set %s for the isolated Redis instance", testCase.variable)
			}
			cache, err := redis.New(redis.Config{URL: endpoint, Cluster: testCase.cluster})
			if err != nil {
				t.Fatal(err)
			}
			defer cache.Close()
			native := openNativeClient(t, endpoint, testCase.cluster)
			ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
			defer cancel()
			key := "kronos-redis-test:" + rand.Text()
			defer func() {
				cleanup, cancelCleanup := context.WithTimeout(context.Background(), time.Second)
				defer cancelCleanup()
				native.Del(cleanup, key)
			}()
			if testCase.cluster {
				assertClusterRouting(t, ctx, cache, native, key)
			}
			if _, found, err := cache.Get(ctx, key); found || err != nil {
				t.Fatalf("Kronos missing key = found %v, %v", found, err)
			}
			if _, err := native.Get(ctx, key).Result(); !errors.Is(err, redisdriver.Nil) {
				t.Fatalf("native missing key = %v", err)
			}
			if err := cache.Set(ctx, key, "", 0); err != nil {
				t.Fatal(err)
			}
			if value, found, err := cache.Get(ctx, key); value != "" || !found || err != nil {
				t.Fatalf("Kronos empty value = %q, %v, %v", value, found, err)
			}
			if value, err := native.Get(ctx, key).Result(); value != "" || err != nil {
				t.Fatalf("native empty value = %q, %v", value, err)
			}
			if err := cache.Delete(ctx, key); err != nil {
				t.Fatal(err)
			}
			const ttl = 500 * time.Millisecond
			if stored, err := cache.SetIfAbsent(ctx, key, "你好", ttl); !stored || err != nil {
				t.Fatalf("Kronos conditional store = %v, %v", stored, err)
			}
			if stored, err := native.SetNX(ctx, key, "next", time.Second).Result(); stored || err != nil {
				t.Fatalf("native conditional store = %v, %v", stored, err)
			}
			if stored, err := cache.SetIfAbsent(ctx, key, "late", time.Second); stored || err != nil {
				t.Fatalf("Kronos replaced an existing value: %v, %v", stored, err)
			}
			if remaining, err := native.PTTL(ctx, key).Result(); err != nil || remaining <= 0 || remaining > ttl {
				t.Fatalf("native TTL = %s, %v", remaining, err)
			}
			if value, err := native.Get(ctx, key).Result(); value != "你好" || err != nil {
				t.Fatalf("native conditional-store value = %q, %v", value, err)
			}
			waitForExpiry(t, ctx, cache, key)
			if _, err := native.Get(ctx, key).Result(); !errors.Is(err, redisdriver.Nil) {
				t.Fatalf("native expired key = %v", err)
			}
			if err := cache.Delete(ctx, key); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := cache.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := cache.Get(ctx, key); !errors.Is(err, redis.ErrClosed) {
				t.Fatalf("Get after Close = %v", err)
			}
		})
	}
}

func assertClusterRouting(t *testing.T, ctx context.Context, cache *redis.Client, native redisdriver.UniversalClient, prefix string) {
	t.Helper()
	slots, err := native.ClusterSlots(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	primaries := make(map[string]bool)
	for _, tag := range []string{"{a}", "{b}", "{c}"} {
		key := prefix + tag
		slot, err := native.ClusterKeySlot(ctx, key).Result()
		if err != nil {
			t.Fatal(err)
		}
		for _, rangeOfSlots := range slots {
			if slot >= int64(rangeOfSlots.Start) && slot <= int64(rangeOfSlots.End) && len(rangeOfSlots.Nodes) > 0 {
				primaries[rangeOfSlots.Nodes[0].Addr] = true
			}
		}
		if err := cache.Set(ctx, key, tag, time.Second); err != nil {
			t.Fatal(err)
		}
		if value, err := native.Get(ctx, key).Result(); err != nil || value != tag {
			t.Fatalf("native shard lookup %s = %q, %v", tag, value, err)
		}
	}
	if len(primaries) != 3 {
		t.Fatalf("fixture keys reached %d primaries, want 3", len(primaries))
	}
}

func openNativeClient(t *testing.T, endpoint string, cluster bool) redisdriver.UniversalClient {
	t.Helper()
	if cluster {
		options, err := redisdriver.ParseClusterURL(endpoint)
		if err != nil {
			t.Fatal("invalid Redis Cluster test URL")
		}
		client := redisdriver.NewClusterClient(options)
		t.Cleanup(func() { client.Close() })
		return client
	}
	options, err := redisdriver.ParseURL(endpoint)
	if err != nil {
		t.Fatal("invalid Redis test URL")
	}
	client := redisdriver.NewClient(options)
	t.Cleanup(func() { client.Close() })
	return client
}

func waitForExpiry(t *testing.T, ctx context.Context, cache *redis.Client, key string) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, found, err := cache.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("Redis TTL did not expire")
		case <-ticker.C:
		}
	}
}

func ExampleNew() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cache, err := redis.New(redis.Config{URL: os.Getenv("KRONOS_REDIS_TEST_URL")})
	if err != nil {
		panic(err)
	}
	defer cache.Close()
	key := "kronos-redis-example:" + rand.Text()
	if err := cache.Set(ctx, key, "你好", time.Second); err != nil {
		panic(err)
	}
	value, found, err := cache.Get(ctx, key)
	if err != nil {
		panic(err)
	}
	fmt.Println(value, found)
	// Output: 你好 true
}
