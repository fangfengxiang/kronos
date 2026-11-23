package order

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/platform/kronos"
)

type discoveryFunc func(context.Context, string, string) ([]string, error)

func (lookup discoveryFunc) Lookup(ctx context.Context, name, protocol string) ([]string, error) {
	return lookup(ctx, name, protocol)
}

func (discoveryFunc) Publish(context.Context, string, string, string) (func(context.Context) error, error) {
	return nil, errors.New("publication should be unused by a client")
}

func TestSharedDiscoverySurvivesCallerCancellation(t *testing.T) {
	var lookups atomic.Int32
	started := make(chan context.Context, 1)
	allowResult := make(chan struct{})
	registry := discoveryFunc(func(ctx context.Context, name, protocol string) ([]string, error) {
		if name != OrderServiceName || protocol != "grpc" {
			t.Errorf("discovery route = %q, %q", name, protocol)
		}
		if lookups.Add(1) == 1 {
			started <- ctx
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-allowResult:
			return nil, nil
		}
	})
	client, err := kronos.NewClient(kronos.Registry(registry))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	orders := NewOrderServiceClient(client)
	first, cancel := context.WithCancel(t.Context())
	defer cancel()
	firstResult := invokeOrder(orders, first)
	lookupContext := awaitOrder(t, started)
	secondResult := invokeOrder(orders, t.Context())
	cancel()
	if err := awaitOrder(t, firstResult); !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller cancellation = %v", err)
	}
	if err := lookupContext.Err(); err != nil {
		t.Fatalf("shared lookup context = %v while another caller can wait", err)
	}
	close(allowResult)
	err = awaitOrder(t, secondResult)
	var failure *kronos.Error
	if !errors.As(err, &failure) || failure.Reason != "NO_INSTANCES" || lookups.Load() != 1 {
		t.Fatalf("second call error = %v after %d lookups", err, lookups.Load())
	}
}

func TestClientCloseCancelsDiscovery(t *testing.T) {
	started := make(chan struct{})
	released := make(chan struct{})
	registry := discoveryFunc(func(ctx context.Context, _, _ string) ([]string, error) {
		close(started)
		<-ctx.Done()
		close(released)
		return nil, ctx.Err()
	})
	client, err := kronos.NewClient(kronos.Registry(registry))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	calls := NewOrderServiceClient(client)
	result := invokeOrder(calls, t.Context())
	awaitOrder(t, started)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	awaitOrder(t, released)
	if err := awaitOrder(t, result); !errors.Is(err, kronos.ErrClientClosed) {
		t.Fatalf("closed client call = %v", err)
	}
	if _, err := calls.GetOrder(t.Context(), &GetOrderRequest{}); !errors.Is(err, kronos.ErrClientClosed) {
		t.Fatalf("call after close = %v", err)
	}
}

func TestClientRejectsUnknownProtocol(t *testing.T) {
	if _, err := kronos.NewClient(kronos.Protocol("other")); !errors.Is(err, kronos.ErrUnsupportedProtocol) {
		t.Fatalf("unsupported protocol = %v", err)
	}
}

func invokeOrder(orders *OrderServiceClient, ctx context.Context) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, err := orders.GetOrder(ctx, &GetOrderRequest{OrderId: "fixture"})
		result <- err
	}()
	return result
}

func awaitOrder[Value any](t *testing.T, result <-chan Value) Value {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Order")
		var zero Value
		return zero
	}
}
