//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/framework/request"
)

func TestOrderPropagatesRequestIDAcrossBothProtocols(t *testing.T) {
	registry := openTestRegistry(t)
	name := "orders.request." + rand.Text()
	runOrderReplica(t, registry, name, requestIDOrder{})
	for _, protocol := range orderProtocols {
		t.Run(string(protocol), func(t *testing.T) {
			waitForInstances(t, registry, name, protocol, 1)
			client := newDiscoveredOrder(t, registry, name, protocol)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			const explicit = "kronos-client_42"
			result, err := client.GetOrder(request.WithID(ctx, explicit), &order.GetOrderRequest{OrderId: "audit"})
			if err != nil || result.GetStatus() != explicit {
				t.Fatalf("request ID = %q, error = %v, want %q", result.GetStatus(), err, explicit)
			}
			observed := map[string]bool{explicit: true}
			for _, callContext := range []context.Context{ctx, request.WithID(ctx, "invalid ID")} {
				result, err := client.GetOrder(callContext, &order.GetOrderRequest{OrderId: "audit"})
				if err != nil {
					t.Fatal(err)
				}
				id := result.GetStatus()
				if id == "" || request.ID(request.WithID(ctx, id)) != id || observed[id] {
					t.Fatalf("generated invalid or repeated request ID %q", id)
				}
				observed[id] = true
			}
		})
	}
}

type requestIDOrder struct{}

func (requestIDOrder) GetOrder(ctx context.Context, input *order.GetOrderRequest) (*order.GetOrderResponse, error) {
	return &order.GetOrderResponse{OrderId: input.GetOrderId(), Status: request.ID(ctx)}, nil
}
