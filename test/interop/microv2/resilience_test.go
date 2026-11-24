//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/infra/etcd"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func TestOrderRecoversBusinessCallsAfterRegistryPartition(t *testing.T) {
	t.Parallel()
	observer := openTestRegistry(t)
	relay := startRegistryRelay(t, os.Getenv("KRONOS_ETCD_TEST_ADDRESS"))
	registry, err := etcd.New(relay.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	name := "orders.partition." + rand.Text()
	process := startOrderProcess(t, relay.listener.Addr().String(), name)
	startupContext, cancelStartup := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelStartup()
	grpcAddress, microAddress := waitForOrderAddresses(t, startupContext, observer, name, process)
	assertTypedOrder(t, registry, name, "confirmed")
	assertTypedOrder(t, registry, name, "confirmed", kronos.Protocol(kronos.ProtocolMicroV2))

	grpcOrders := newDiscoveredOrder(t, registry, name, kronos.ProtocolGRPC)
	microOrders := newDiscoveredOrder(t, registry, name, kronos.ProtocolMicroV2)
	relay.setPaused(true)
	assertBoundedDiscoveryFailure(t, grpcOrders)
	expiryContext, cancelExpiry := context.WithTimeout(t.Context(), 24*time.Second)
	defer cancelExpiry()
	waitForInstanceCount(t, expiryContext, observer, name, kronos.ProtocolGRPC, 0)
	waitForInstanceCount(t, expiryContext, observer, name, kronos.ProtocolMicroV2, 0)
	assertOrderOverGRPC(t, grpcAddress)

	relay.setPaused(false)
	recoveryContext, cancelRecovery := context.WithTimeout(t.Context(), 22*time.Second)
	defer cancelRecovery()
	recoveredGRPC, recoveredMicro := waitForOrderAddresses(t, recoveryContext, observer, name, process)
	if recoveredGRPC != grpcAddress || recoveredMicro != microAddress {
		t.Fatalf("recovered endpoints = %s and %s, want %s and %s", recoveredGRPC, recoveredMicro, grpcAddress, microAddress)
	}
	awaitRecoveredOrder(t, recoveryContext, grpcOrders, kronos.ProtocolGRPC)
	awaitRecoveredOrder(t, recoveryContext, microOrders, kronos.ProtocolMicroV2)

	process.stop(t, 12*time.Second)
	logs, err := os.ReadFile(process.logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"registry_renew_failed", "registry_lease_lost", "registry_republished", "registry_renew_recovered"} {
		if !strings.Contains(string(logs), "event="+event+" ") {
			t.Errorf("missing %q from Order logs", event)
		}
	}
	waitForInstances(t, observer, name, kronos.ProtocolGRPC, 0)
	waitForInstances(t, observer, name, kronos.ProtocolMicroV2, 0)
}

func TestOrderCrashExpiresBothProtocolAddresses(t *testing.T) {
	t.Parallel()
	registry := openTestRegistry(t)
	name := "orders.crash." + rand.Text()
	process := startOrderProcess(t, os.Getenv("KRONOS_ETCD_TEST_ADDRESS"), name)
	startupContext, cancelStartup := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelStartup()
	waitForOrderAddresses(t, startupContext, registry, name, process)
	assertTypedOrder(t, registry, name, "confirmed")

	if err := process.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.done:
		if process.err == nil {
			t.Fatal("killed test process exited successfully")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("killed test process did not exit")
	}
	for _, protocol := range []kronos.WireProtocol{kronos.ProtocolGRPC, kronos.ProtocolMicroV2} {
		addresses, err := registry.Lookup(t.Context(), name, string(protocol))
		if err != nil || len(addresses) != 1 {
			t.Fatalf("%s address immediately after crash = %v, %v", protocol, addresses, err)
		}
	}
	expiryContext, cancelExpiry := context.WithTimeout(t.Context(), 24*time.Second)
	defer cancelExpiry()
	for _, protocol := range []kronos.WireProtocol{kronos.ProtocolGRPC, kronos.ProtocolMicroV2} {
		waitForInstanceCount(t, expiryContext, registry, name, protocol, 0)
		client, err := kronos.NewClient(kronos.Registry(registry), kronos.Name(name), kronos.Protocol(protocol))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		_, err = order.NewOrderServiceClient(client).GetOrder(expiryContext, &order.GetOrderRequest{OrderId: "after-crash"})
		var failure *kronos.Error
		if !errors.As(err, &failure) || failure.Code != kronos.Unavailable || failure.Reason != "NO_INSTANCES" {
			t.Errorf("%s call after expiry = %v, want NO_INSTANCES", protocol, err)
		}
	}
}

func assertBoundedDiscoveryFailure(t *testing.T, orders *order.OrderServiceClient) {
	t.Helper()
	const concurrentCalls = 12
	results := make(chan error, concurrentCalls)
	for range concurrentCalls {
		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), 350*time.Millisecond)
			defer cancel()
			_, err := orders.GetOrder(ctx, &order.GetOrderRequest{OrderId: "during-partition"})
			results <- err
		}()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for range concurrentCalls {
		select {
		case err := <-results:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("call during discovery outage = %v, want DeadlineExceeded", err)
			}
		case <-ctx.Done():
			t.Fatal("discovery callers did not return within their budgets")
		}
	}
	_, err := orders.GetOrder(ctx, &order.GetOrderRequest{OrderId: "after-shared-lookup-timeout"})
	var failure *kronos.Error
	if !errors.As(err, &failure) || failure.Code != kronos.Unavailable || failure.Reason != "DISCOVERY_FAILED" {
		t.Fatalf("shared query failure = %v, want DISCOVERY_FAILED", err)
	}
}

func awaitRecoveredOrder(t *testing.T, ctx context.Context, orders *order.OrderServiceClient, protocol kronos.WireProtocol) {
	t.Helper()
	retry := time.NewTicker(100 * time.Millisecond)
	defer retry.Stop()
	const orderID = "after-registry-recovery"
	for {
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		result, err := orders.GetOrder(attempt, &order.GetOrderRequest{OrderId: orderID})
		cancel()
		if err == nil {
			if result.GetOrderId() != orderID || result.GetStatus() != "confirmed" {
				t.Fatalf("%s call after recovery returned %v", protocol, result)
			}
			return
		}
		var failure *kronos.Error
		if !errors.Is(err, context.DeadlineExceeded) && (!errors.As(err, &failure) || failure.Code != kronos.Unavailable) {
			t.Fatalf("%s call after recovery returned %v", protocol, err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s did not recover its Order call before the deadline: %v", protocol, err)
		case <-retry.C:
		}
	}
}

// registryRelay disconnects only the etcd clients that the test routes through it.
type registryRelay struct {
	listener   net.Listener
	endpoint   string
	mu         sync.Mutex
	paused     bool
	clients    map[net.Conn]struct{}
	workers    sync.WaitGroup
	acceptDone chan struct{}
}

func startRegistryRelay(t *testing.T, endpoint string) *registryRelay {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relay := &registryRelay{
		listener: listener, endpoint: endpoint, clients: make(map[net.Conn]struct{}), acceptDone: make(chan struct{}),
	}
	go relay.accept(t.Context())
	t.Cleanup(func() {
		relay.setPaused(true)
		_ = listener.Close()
		<-relay.acceptDone
		relay.workers.Wait()
	})
	return relay
}

func (relay *registryRelay) setPaused(paused bool) {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	relay.paused = paused
	if paused {
		for client := range relay.clients {
			_ = client.Close()
		}
	}
}

func (relay *registryRelay) accept(ctx context.Context) {
	defer close(relay.acceptDone)
	for {
		client, err := relay.listener.Accept()
		if err != nil {
			return
		}
		relay.mu.Lock()
		if relay.paused {
			relay.mu.Unlock()
			_ = client.Close()
			continue
		}
		relay.clients[client] = struct{}{}
		relay.mu.Unlock()
		relay.workers.Go(func() {
			defer func() {
				_ = client.Close()
				relay.mu.Lock()
				delete(relay.clients, client)
				relay.mu.Unlock()
			}()
			dialer := net.Dialer{Timeout: time.Second}
			upstream, err := dialer.DialContext(ctx, "tcp", relay.endpoint)
			if err != nil {
				return
			}
			defer upstream.Close()
			forwardDone := make(chan struct{})
			go func() {
				_, _ = io.Copy(upstream, client)
				_ = upstream.Close()
				close(forwardDone)
			}()
			_, _ = io.Copy(client, upstream)
			_ = client.Close()
			<-forwardDone
		})
	}
}
