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
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
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

func TestDiscoveredOrderBoundsRetriesByProtobufMethod(t *testing.T) {
	registry := openTestRegistry(t)
	orderContract := (&order.GetOrderRequest{}).ProtoReflect().Descriptor().ParentFile()
	readMethod := orderContract.Services().ByName("OrderService").Methods().ByName("GetOrder")
	unmarkedContract := protodesc.ToFileDescriptorProto(orderContract)
	unmarkedContract.Service[readMethod.Parent().Index()].Method[readMethod.Index()].Options = nil
	unmarkedFile, err := protodesc.NewFile(unmarkedContract, nil)
	if err != nil {
		t.Fatal(err)
	}
	unmarkedMethod := unmarkedFile.Services().ByName("OrderService").Methods().ByName("GetOrder")

	for _, protocol := range orderProtocols {
		t.Run(string(protocol), func(t *testing.T) {
			name := "orders.retry-limit." + rand.Text()
			replicas := make(map[string]*orderReplica, 3)
			for _, label := range []string{"a", "b", "c"} {
				replica := &orderReplica{label: label}
				replicas[label] = replica
				runOrderReplica(t, registry, name, replica)
			}
			waitForInstances(t, registry, name, protocol, 3)
			options := []kronos.Option{kronos.Registry(registry), kronos.Name(name), kronos.Protocol(protocol)}
			roundRobinClient := newRetryClient(t, options)
			orders := order.NewOrderServiceClient(roundRobinClient)
			var inSelectionOrder [3]*orderReplica
			for i := range inSelectionOrder {
				label := getOrderStatus(t, orders)
				replica := replicas[label]
				if replica == nil {
					t.Fatalf("round robin returned a missing or repeated replica %q", label)
				}
				delete(replicas, label)
				inSelectionOrder[i] = replica
			}
			inSelectionOrder[0].unavailable.Store(true)
			inSelectionOrder[1].unavailable.Store(true)

			for _, policy := range []struct {
				name  string
				rpc   protoreflect.MethodDescriptor
				calls [3]int32
			}{
				{name: "unmarked", rpc: unmarkedMethod, calls: [3]int32{1, 0, 0}},
				{name: "read_only", rpc: readMethod, calls: [3]int32{1, 1, 0}},
			} {
				t.Run(policy.name, func(t *testing.T) {
					client := newRetryClient(t, options)
					var baseline [3]int32
					for i, replica := range inSelectionOrder {
						baseline[i] = replica.calls.Load()
					}
					ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
					defer cancel()
					err := client.Invoke(ctx, policy.rpc, &order.GetOrderRequest{OrderId: "retry-limit"}, new(order.GetOrderResponse))
					var failure *kronos.Error
					if !errors.As(err, &failure) || failure.Code != kronos.Unavailable || failure.Reason != "SERVICE_UNAVAILABLE" {
						t.Fatalf("call error = %v, want SERVICE_UNAVAILABLE", err)
					}
					var calls [3]int32
					for i, replica := range inSelectionOrder {
						calls[i] = replica.calls.Load() - baseline[i]
					}
					if calls != policy.calls {
						t.Errorf("replica calls = %v, want %v", calls, policy.calls)
					}
				})
			}
		})
	}
}

func newRetryClient(t *testing.T, options []kronos.Option) *kronos.Client {
	t.Helper()
	client, err := kronos.NewClient(options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
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
