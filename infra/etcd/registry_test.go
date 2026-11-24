package etcd

import (
	"crypto/tls"
	"errors"
	"slices"
	"testing"
)

func TestRejectPlaintextEndpointInTLSConfig(t *testing.T) {
	registry, err := NewWithConfig(Config{
		Endpoints: []string{"https://localhost:2379", "HTTP://localhost:2380"},
		TLS:       &tls.Config{},
	})
	if registry != nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("mixed TLS endpoints = %v, %v", registry, err)
	}
}

func TestRejectInvalidRegistryInputs(t *testing.T) {
	for _, endpoint := range []string{
		"", "localhost", ":2379", "0.0.0.0:2379", "[::]:2379", "localhost:0",
		"https://user:pass@localhost:2379", "http://localhost:2379/api", "http://localhost:2379/?",
	} {
		if registry, err := New(endpoint); registry != nil || !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("New(%q) = %v, %v", endpoint, registry, err)
		}
	}
	registry := new(Registry)
	for _, serviceName := range []string{"", "orders/other", ".", ".."} {
		if _, err := registry.Lookup(t.Context(), serviceName, "grpc"); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("Lookup(%q) = %v", serviceName, err)
		}
		if _, err := registry.Publish(t.Context(), serviceName, "localhost:9090", ""); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("Publish(%q) = %v", serviceName, err)
		}
	}
	if _, err := registry.Lookup(t.Context(), "orders", "http"); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("unknown protocol: %v", err)
	}
	if _, err := registry.Publish(t.Context(), "orders", "", ""); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing protocol addresses: %v", err)
	}
}

func TestEncodeProtocolRecords(t *testing.T) {
	const service = "go.micro.srv.order"
	const instanceID = "node1"
	const grpcAddress = "order.example:9090"
	const microRPCAddress = "[::1]:9091"
	want := []instanceRecord{
		{key: "/kronos/registry/grpc/go.micro.srv.order/node1", value: grpcAddress},
		{key: "/micro/registry/go.micro.srv.order/node1", value: `{"name":"go.micro.srv.order","version":"1.0.0","nodes":[{"id":"node1","address":"[::1]:9091"}]}`},
	}
	if records := instanceRecords(service, instanceID, grpcAddress, microRPCAddress); !slices.Equal(records, want) {
		t.Fatalf("published records = %v, want %v", records, want)
	}
}
