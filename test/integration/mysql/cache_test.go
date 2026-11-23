//go:build mysql_integration && redis_integration && (linux || darwin)

package mysql_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/infra/redis"
)

func TestTextBackendCachesMySQLProfiles(t *testing.T) {
	binary := buildTextBackend(t)
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
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			const firstPrefix = "缓存前："
			const secondPrefix = "更新后："
			const prompt = "你好"
			profile := insertProfile(t, ctx, firstPrefix)
			cache, err := redis.New(redis.Config{URL: endpoint, Cluster: testCase.cluster})
			if err != nil {
				t.Fatal(err)
			}
			defer cache.Close()
			key := "kronos:textbackend:profiles/integration/" + profile.name
			if _, found, err := cache.Get(ctx, key); found || err != nil {
				t.Fatalf("initial cache = %v, %v", found, err)
			}
			flags := []string{"-profile-cache-ttl", "2s", "-profile-cache-namespace", "integration"}
			if testCase.cluster {
				flags = append(flags, "-profile-cache-cluster")
			}
			backend := profile.startBackend(t, ctx, binary, flags, "KRONOS_TEXTBACKEND_REDIS_URL="+endpoint)
			input := &textbackend.GenerateRequest{Prompt: prompt}
			waitForProfile(t, ctx, backend.done, backend.client, input, firstPrefix+prompt)
			assertCachedPrefix(t, ctx, cache, key, firstPrefix)
			profile.update(t, ctx, secondPrefix)
			response, err := backend.client.Generate(ctx, input)
			if err != nil || response.GetText() != firstPrefix+prompt {
				t.Fatalf("cached generation = %v, %v", response, err)
			}
			waitForUpdatedPrefix(t, ctx, backend.client, input, firstPrefix+prompt, secondPrefix+prompt)
			assertCachedPrefix(t, ctx, cache, key, secondPrefix)
			backend.stop(t, ctx)
		})
	}
}

func TestTextBackendUsesMySQLWhenRedisStalls(t *testing.T) {
	binary := buildTextBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptTCP()
		accepted <- err
		if err == nil {
			defer connection.Close()
			<-ctx.Done()
		}
	}()
	const prefix = "数据库："
	const updatedPrefix = "实时："
	const prompt = "你好"
	profile := insertProfile(t, ctx, prefix)
	flags := []string{"-profile-cache-ttl", "2s"}
	backend := profile.startBackend(t, ctx, binary, flags, "KRONOS_TEXTBACKEND_REDIS_URL=redis://"+listener.Addr().String())
	input := &textbackend.GenerateRequest{Prompt: prompt}
	waitForProfile(t, ctx, backend.done, backend.client, input, prefix+prompt)
	select {
	case err := <-accepted:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("TextBackend did not contact Redis")
	}
	profile.update(t, ctx, updatedPrefix)
	callContext, cancelCall := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancelCall()
	response, err := backend.client.Generate(callContext, input)
	if err != nil || response.GetText() != updatedPrefix+prompt {
		t.Fatalf("generation during Redis stall = %v, %v", response, err)
	}
	backend.stop(t, ctx)
}

func assertCachedPrefix(t *testing.T, ctx context.Context, cache *redis.Client, key, expected string) {
	t.Helper()
	value, found, err := cache.Get(ctx, key)
	if err != nil || !found || value != expected {
		t.Fatalf("cached prefix = %q, found %v, error %v", value, found, err)
	}
}

func waitForUpdatedPrefix(t *testing.T, ctx context.Context, client *textbackend.Client, input *textbackend.GenerateRequest, old, expected string) {
	t.Helper()
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	for {
		response, err := client.Generate(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		switch response.GetText() {
		case expected:
			return
		case old:
		default:
			t.Fatalf("generation while waiting for TTL = %q", response.GetText())
		}
		select {
		case <-ctx.Done():
			t.Fatal("the cached profile did not expire")
		case <-retry.C:
		}
	}
}
