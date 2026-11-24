//go:build consul_integration && (linux || darwin)

package consul_test

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/infra/consul"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func TestOrderUsesKronosWithConsul(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	endpoint, _ := startConsulAgent(t, ctx)
	registry, err := consul.NewRPCRegistry(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	serviceName := "orders.consul." + rand.Text()
	registryOption := kronos.Registry(registry)
	if _, err := kronos.New(kronos.Name(serviceName), registryOption, kronos.GRPC("127.0.0.1:0"), kronos.MicroRPC("127.0.0.1:0")); !errors.Is(err, kronos.ErrUnsupportedProtocol) {
		t.Fatalf("Consul dual-protocol application = %v", err)
	}
	if _, err := kronos.NewClient(registryOption, kronos.Protocol(kronos.ProtocolMicroV2)); !errors.Is(err, kronos.ErrUnsupportedProtocol) {
		t.Fatalf("Consul MicroRPC client = %v", err)
	}
	application, err := kronos.New(kronos.Name(serviceName), registryOption, kronos.GRPC("127.0.0.1:0"))
	if err != nil {
		t.Fatal(err)
	}
	if err := application.Register(order.BindOrderService(order.NewService())); err != nil {
		t.Fatal(err)
	}
	appContext, stop := context.WithCancel(ctx)
	stopped := make(chan struct{})
	var runError error
	go func() {
		runError = application.Run(appContext)
		close(stopped)
	}()
	defer func() {
		stop()
		select {
		case <-stopped:
			if runError != nil {
				t.Error(runError)
			}
		case <-time.After(12 * time.Second):
			t.Error("Order did not close")
		}
	}()
	await(t, ctx, "healthy Order instance in Consul", func() bool {
		select {
		case <-stopped:
			t.Fatalf("Order stopped before becoming discoverable: %v", runError)
		default:
		}
		addresses, err := registry.Lookup(ctx, serviceName, string(kronos.ProtocolGRPC))
		return err == nil && len(addresses) == 1
	})
	client, err := kronos.NewClient(registryOption, kronos.Name(serviceName))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	response, err := order.NewOrderServiceClient(client).GetOrder(ctx, &order.GetOrderRequest{OrderId: "consul-order"})
	if err != nil || response.GetOrderId() != "consul-order" || response.GetStatus() != "confirmed" {
		t.Fatalf("discovered Order returned %v, %v", response, err)
	}
	stop()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("Order did not withdraw in time")
	}
	addresses, err := registry.Lookup(ctx, serviceName, string(kronos.ProtocolGRPC))
	if err != nil || len(addresses) != 0 {
		t.Fatalf("addresses after withdrawal: %v, %v", addresses, err)
	}
	var registered map[string]struct{ Service string }
	getConsulJSON(t, ctx, endpoint+"/v1/agent/services", &registered)
	for _, entry := range registered {
		if entry.Service == serviceName {
			t.Fatal("Consul still lists the stopped service")
		}
	}
}
