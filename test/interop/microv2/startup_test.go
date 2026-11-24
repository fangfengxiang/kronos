//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/infra/etcd"
	"github.com/fangfengxiang/kronos/infra/microv2"
	"google.golang.org/protobuf/proto"
)

func TestOrderRollsBackWhenInitialPublicationFails(t *testing.T) {
	t.Parallel()
	observer := openTestRegistry(t)
	relay := startRegistryRelay(t, os.Getenv("KRONOS_ETCD_TEST_ADDRESS"))
	relay.setPaused(true)
	registry, err := etcd.New(relay.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	publisher := &gatedPublication{
		Registry: registry,
		bound:    make(chan startupAddresses, 1),
		proceed:  make(chan struct{}),
	}
	name := "orders.startup." + rand.Text()
	running := runOrderReplica(t, publisher, name, order.NewService())
	listeners := awaitOrderSignal(t, publisher.bound)
	if listeners.grpc == "" || listeners.microRPC == "" || listeners.grpc == listeners.microRPC {
		t.Fatalf("addresses passed for initial publication: %+v", listeners)
	}
	assertOrderOverGRPC(t, listeners.grpc)
	assertOrderOverMicroRPC(t, name, listeners.microRPC)

	close(publisher.proceed)
	if err := running.wait(t); !errors.Is(err, context.DeadlineExceeded) ||
		!strings.Contains(err.Error(), "publish application readiness") {
		t.Fatalf("startup result = %v, want a publication deadline failure", err)
	}
	for _, address := range []string{listeners.grpc, listeners.microRPC} {
		assertOrderPortReleased(t, address)
	}
	lookup, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for _, protocol := range orderProtocols {
		addresses, err := observer.Lookup(lookup, name, string(protocol))
		if err != nil || len(addresses) != 0 {
			t.Fatalf("discover failed startup on %s: %v, %v", protocol, addresses, err)
		}
	}
}

type startupAddresses struct {
	grpc     string
	microRPC string
}

type gatedPublication struct {
	*etcd.Registry
	bound   chan startupAddresses
	proceed chan struct{}
}

func (publisher *gatedPublication) Publish(ctx context.Context, name, grpcAddress, microAddress string) (func(context.Context) error, error) {
	publisher.bound <- startupAddresses{grpc: grpcAddress, microRPC: microAddress}
	select {
	case <-publisher.proceed:
		return publisher.Registry.Publish(ctx, name, grpcAddress, microAddress)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func assertOrderOverMicroRPC(t *testing.T, name, address string) {
	t.Helper()
	client, err := microv2.NewClient(address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	const id = "before-publication"
	request, err := proto.Marshal(&order.GetOrderRequest{OrderId: id})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	payload, err := client.Call(ctx, name, "OrderService.GetOrder", request)
	if err != nil {
		t.Fatal(err)
	}
	var result order.GetOrderResponse
	if err := proto.Unmarshal(payload, &result); err != nil || result.GetOrderId() != id || result.GetStatus() != "confirmed" {
		t.Fatalf("MicroRPC before publication returned %v, %v", &result, err)
	}
}
