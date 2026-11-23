package kronos

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/platform/kronos/internal/transport"
)

func TestDiscoverySharesOneSignalAcrossExpiredWaiters(t *testing.T) {
	var lookups atomic.Int32
	started := make(chan struct{})
	proceed := make(chan struct{})
	registry := lookupRegistry(func(ctx context.Context, _, _ string) ([]string, error) {
		if lookups.Add(1) == 1 {
			close(started)
		}
		select {
		case <-proceed:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	client := makeDiscoveryClient(t, registry)
	const serviceName = "orders"
	first := make(chan error, 1)
	go func() { first <- client.resolve(t.Context(), serviceName) }()
	awaitDiscoverySignal(t, started)
	client.mu.Lock()
	initialSignal := client.views[serviceName].lookupDone
	client.mu.Unlock()
	for range 64 {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Millisecond)
		err := client.resolve(ctx, serviceName)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expired discovery waiter returned %v", err)
		}
	}
	client.mu.Lock()
	sameSignal := initialSignal != nil && client.views[serviceName].lookupDone == initialSignal
	client.mu.Unlock()
	if !sameSignal || lookups.Load() != 1 {
		t.Fatalf("discovery during outage used shared signal %t and made %d queries", sameSignal, lookups.Load())
	}
	close(proceed)
	if err := awaitDiscoverySignal(t, first); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.views[serviceName].lookupDone != nil {
		t.Fatal("completed lookup signal still belongs to the service view")
	}
}

func TestDiscoveryRetiresConnectionsAfterTheirLastUse(t *testing.T) {
	const visibleAddress = "127.0.0.1:1001"
	const serviceName = "orders"
	registry := lookupRegistry(func(context.Context, string, string) ([]string, error) {
		return []string{visibleAddress}, nil
	})
	client := makeDiscoveryClient(t, registry)
	var activeCloses, visibleCloses atomic.Int32
	idleClosed := make(chan struct{})
	retiredIdle := &peer{address: "127.0.0.1:1002", connection: transport.Client{
		Close: func() error { close(idleClosed); return nil },
	}}
	retiredActive := &peer{address: "127.0.0.1:1003", uses: 2, connection: transport.Client{
		Close: func() error { activeCloses.Add(1); return nil },
	}}
	visible := &peer{address: visibleAddress, connection: transport.Client{
		Close: func() error { visibleCloses.Add(1); return nil },
	}}
	view := &serviceView{connections: make(map[string]*peer)}
	for _, instance := range []*peer{retiredIdle, retiredActive, visible} {
		view.connections[instance.address] = instance
		client.peers[instance] = struct{}{}
	}
	client.views[serviceName] = view
	if err := client.resolve(t.Context(), serviceName); err != nil {
		t.Fatal(err)
	}
	awaitDiscoverySignal(t, idleClosed)
	if _, present := view.connections[retiredActive.address]; present || activeCloses.Load() != 0 {
		t.Fatal("active retired connection remained eligible or closed before release")
	}
	client.release(retiredActive, false)
	if activeCloses.Load() != 0 {
		t.Fatal("active retired connection closed with a user remaining")
	}
	client.release(retiredActive, false)
	if activeCloses.Load() != 1 {
		t.Fatal("retired connection did not close on last release")
	}
	selected, err := client.acquire(t.Context(), serviceName, "")
	if err != nil || selected != visible {
		t.Fatalf("active connection was not reused: %v, %v", selected, err)
	}
	client.release(selected, false)
	for range 2 {
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if activeCloses.Load() != 1 || visibleCloses.Load() != 1 {
		t.Fatalf("connections closed %d and %d times", activeCloses.Load(), visibleCloses.Load())
	}
}

type lookupRegistry func(context.Context, string, string) ([]string, error)

func (lookup lookupRegistry) Lookup(ctx context.Context, service, protocol string) ([]string, error) {
	return lookup(ctx, service, protocol)
}

func (lookupRegistry) Publish(context.Context, string, string, string) (func(context.Context) error, error) {
	return nil, errors.New("unexpected publication in client test")
}

func makeDiscoveryClient(t *testing.T, provider RegistryProvider) *Client {
	t.Helper()
	client, err := NewClient(Registry(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func awaitDiscoverySignal[Value any](t *testing.T, signal <-chan Value) Value {
	t.Helper()
	select {
	case result := <-signal:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for discovery")
		var zero Value
		return zero
	}
}
