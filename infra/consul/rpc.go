package consul

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

// RPCRegistry publishes and discovers healthy gRPC instances for Kronos.
type RPCRegistry struct {
	registry *Registry
}

// NewRPCRegistry builds a registry for use with kronos.Registry.
// The caller owns the returned registry.
func NewRPCRegistry(endpoint string) (*RPCRegistry, error) {
	registry, err := New(endpoint)
	if err != nil {
		return nil, err
	}
	return &RPCRegistry{registry: registry}, nil
}

// Close releases the underlying client's idle connections.
func (registry *RPCRegistry) Close() {
	registry.registry.Close()
}

// SupportsProtocol reports whether Consul can publish and discover the wire protocol.
func (*RPCRegistry) SupportsProtocol(protocol string) bool {
	return protocol == grpcTag
}

// Publish registers one gRPC instance and returns its withdrawal function.
func (registry *RPCRegistry) Publish(ctx context.Context, name, grpcAddress, microRPCAddress string) (func(context.Context) error, error) {
	if microRPCAddress != "" || grpcAddress == "" {
		return nil, errors.New("Consul publication requires a gRPC address and supports only gRPC")
	}
	instance := Instance{ID: name + "-" + rand.Text(), Name: name, Address: grpcAddress}
	withdraw := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return registry.registry.Deregister(ctx, instance.ID)
	}
	registerContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := registry.registry.Register(registerContext, instance); err != nil {
		return nil, errors.Join(err, withdraw(context.WithoutCancel(ctx)))
	}
	return withdraw, nil
}

// Lookup returns the healthy addresses for a gRPC service name.
func (registry *RPCRegistry) Lookup(ctx context.Context, name, protocol string) ([]string, error) {
	if !registry.SupportsProtocol(protocol) {
		return nil, fmt.Errorf("Consul discovery supports only gRPC, got %q", protocol)
	}
	return registry.registry.Lookup(ctx, name)
}
