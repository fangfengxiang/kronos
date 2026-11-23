//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/infra/etcd"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func TestDiscoveredOrderRotatesAndBypassesAnUnavailableReplica(t *testing.T) {
	registry := openTestRegistry(t)
	for _, protocol := range []kronos.WireProtocol{kronos.ProtocolGRPC, kronos.ProtocolMicroV2} {
		t.Run(string(protocol), func(t *testing.T) {
			name := "orders.replicas." + rand.Text()
			first := &orderReplica{label: "first"}
			second := &orderReplica{label: "second"}
			firstRun := runOrderReplica(t, registry, name, first)
			secondRun := runOrderReplica(t, registry, name, second)
			waitForInstances(t, registry, name, protocol, 2)

			client, err := kronos.NewClient(kronos.Registry(registry), kronos.Name(name), kronos.Protocol(protocol))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			orders := order.NewOrderServiceClient(client)
			firstResult := getOrderStatus(t, orders)
			secondResult := getOrderStatus(t, orders)
			if firstResult == secondResult || first.calls.Load() != 1 || second.calls.Load() != 1 {
				t.Fatalf("round robin returned %q, %q with counts %d, %d", firstResult, secondResult, first.calls.Load(), second.calls.Load())
			}
			failed, failedRun := first, firstRun
			if firstResult == second.label {
				failed, failedRun = second, secondRun
			}
			failed.unavailable.Store(true)
			if got := getOrderStatus(t, orders); got != secondResult || failed.calls.Load() != 2 {
				t.Fatalf("idempotent fallback = %q after %d calls to failed instance", got, failed.calls.Load())
			}
			if got := getOrderStatus(t, orders); got != secondResult || failed.calls.Load() != 2 {
				t.Fatalf("cooldown selection = %q after %d calls to failed instance", got, failed.calls.Load())
			}
			failedRun.stop()
			waitForInstances(t, registry, name, protocol, 1)
			if got := getOrderStatus(t, orders); got != secondResult {
				t.Fatalf("call after withdrawal = %q, want %q", got, secondResult)
			}
		})
	}
}

type orderReplica struct {
	label       string
	calls       atomic.Int32
	unavailable atomic.Bool
}

func (replica *orderReplica) GetOrder(_ context.Context, request *order.GetOrderRequest) (*order.GetOrderResponse, error) {
	replica.calls.Add(1)
	if replica.unavailable.Load() {
		return nil, &kronos.Error{Code: kronos.Unavailable, Reason: "SERVICE_UNAVAILABLE", Message: "replica is unavailable"}
	}
	return &order.GetOrderResponse{OrderId: request.GetOrderId(), Status: replica.label}, nil
}

func openTestRegistry(t *testing.T) *etcd.Registry {
	t.Helper()
	endpoint := os.Getenv("KRONOS_ETCD_TEST_ADDRESS")
	if endpoint == "" {
		t.Fatal("set KRONOS_ETCD_TEST_ADDRESS to an isolated etcd host:port")
	}
	registry, err := etcd.New(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	return registry
}

type runningOrder struct {
	stop    context.CancelFunc
	done    chan struct{}
	err     error
	checked bool
}

func (running *runningOrder) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-running.done:
		running.checked = true
		return running.err
	case <-time.After(12 * time.Second):
		t.Fatal("Order replica did not release its endpoints")
		return nil
	}
}

func runOrderReplica(t *testing.T, registry kronos.RegistryProvider, name string, implementation order.OrderServiceUnaryService, options ...kronos.Option) *runningOrder {
	t.Helper()
	options = append([]kronos.Option{kronos.Name(name), kronos.Registry(registry), kronos.GRPC("127.0.0.1:0"), kronos.MicroRPC("127.0.0.1:0")}, options...)
	application, err := kronos.New(options...)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.Register(order.BindOrderService(implementation)); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	running := &runningOrder{stop: stop, done: make(chan struct{})}
	go func() {
		running.err = application.Run(ctx)
		close(running.done)
	}()
	t.Cleanup(func() {
		stop()
		if !running.checked {
			if err := running.wait(t); err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		}
	})
	return running
}

func waitForInstances(t *testing.T, registry *etcd.Registry, name string, protocol kronos.WireProtocol, count int) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	return waitForInstanceCount(t, ctx, registry, name, protocol, count)
}

func waitForInstanceCount(t *testing.T, ctx context.Context, registry *etcd.Registry, name string, protocol kronos.WireProtocol, count int) []string {
	t.Helper()
	check := time.NewTicker(25 * time.Millisecond)
	defer check.Stop()
	for {
		addresses, err := registry.Lookup(ctx, name, string(protocol))
		if err != nil {
			t.Fatal(err)
		}
		if len(addresses) == count {
			return addresses
		}
		select {
		case <-check.C:
		case <-ctx.Done():
			t.Fatalf("%s discovered %v, want %d replicas", protocol, addresses, count)
		}
	}
}

func getOrderStatus(t *testing.T, orders *order.OrderServiceClient) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	result, err := orders.GetOrder(ctx, &order.GetOrderRequest{OrderId: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return result.GetStatus()
}
