package binding_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	wire "github.com/fangfengxiang/kronos/examples/textbackend/internal/wire/kronos/textbackend/v1"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
)

func TestDiscoveredClientMatchesRoundRobinAndClearsRemovedEndpoints(t *testing.T) {
	first := startBindingServer(t, labelService("first"))
	second := startBindingServer(t, labelService("second"))
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	updates := make(chan discoveryUpdate)
	stopped := make(chan struct{})
	discovery := &testDiscovery{updates: updates, stopped: stopped}
	construction, cancelConstruction := context.WithCancel(ctx)
	client, err := textbackend.NewDiscoveredGRPCClient(construction, textbackend.DefaultServiceName, time.Second, discovery)
	cancelConstruction()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	standardResolver := manual.NewBuilderWithScheme("reference")
	standard, err := grpc.NewClient("reference:///textbackend", grpc.WithResolvers(standardResolver), grpc.WithNoProxy(),
		grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { standard.Close() })
	standard.Connect()
	nativeClient := wire.NewTextBackendServiceClient(standard)
	invoke := func(ctx context.Context) (string, error) {
		response, err := client.Generate(ctx, &textbackend.GenerateRequest{Prompt: "test"})
		return response.GetText(), err
	}
	standardInvoke := func(ctx context.Context) (string, error) {
		response, err := nativeClient.Generate(ctx, &wire.GenerateRequest{Prompt: "test"})
		return response.GetText(), err
	}
	publish := func(addresses ...string) {
		update := discoveryUpdate{addresses: addresses, applied: make(chan struct{})}
		select {
		case updates <- update:
		case <-ctx.Done():
			t.Fatal("could not send discovery snapshot")
		}
		select {
		case <-update.applied:
		case <-ctx.Done():
			t.Fatal("discovery snapshot was not applied")
		}
		state := resolver.State{}
		for _, address := range addresses {
			state.Addresses = append(state.Addresses, resolver.Address{Addr: address})
		}
		standardResolver.UpdateState(state)
	}
	assertUnavailable := func() {
		t.Helper()
		if _, err := invoke(ctx); !isPublicUnavailable(err) {
			t.Fatalf("Kronos empty snapshot = %v", err)
		}
		if _, err := standardInvoke(ctx); status.Code(err) != codes.Unavailable {
			t.Fatalf("native empty snapshot = %v", err)
		}
	}
	publish()
	assertUnavailable()
	publish(first, second)
	waitForReplies(t, ctx, invoke, "first", "second")
	waitForReplies(t, ctx, standardInvoke, "first", "second")
	publish()
	for range 4 {
		assertUnavailable()
	}
	publish(second)
	waitForReplies(t, ctx, invoke, "second")
	waitForReplies(t, ctx, standardInvoke, "second")
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("Close returned before discovery stopped")
	}
}

func isPublicUnavailable(err error) bool {
	var failure *rpc.Error
	return errors.As(err, &failure) && failure.Code == rpc.Unavailable && failure.Reason == rpc.ServiceUnavailableReason
}

func waitForReplies(t *testing.T, ctx context.Context, call func(context.Context) (string, error), expected ...string) {
	t.Helper()
	remaining := make(map[string]bool)
	for _, reply := range expected {
		remaining[reply] = true
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for len(remaining) > 0 {
		reply, err := call(ctx)
		if err == nil {
			delete(remaining, reply)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("missing replies %v, last result %q, %v", remaining, reply, err)
		case <-ticker.C:
		}
	}
}

func labelService(label string) generateFunc {
	return func(context.Context, *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error) {
		return &textbackend.GenerateResponse{Text: label}, nil
	}
}

type discoveryUpdate struct {
	addresses []string
	applied   chan struct{}
}

type testDiscovery struct {
	updates <-chan discoveryUpdate
	stopped chan<- struct{}
}

func (discovery *testDiscovery) Watch(ctx context.Context, serviceName string, observe func([]string)) error {
	defer close(discovery.stopped)
	if serviceName != textbackend.DefaultServiceName {
		return fmt.Errorf("unexpected service name %q", serviceName)
	}
	for {
		select {
		case update := <-discovery.updates:
			observe(update.addresses)
			close(update.applied)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
