// Package kronos assembles services and transports from public Protobuf
// contracts.
package kronos

import (
	"context"
	"errors"
	"time"
)

// RegistryProvider publishes and resolves instances. Its creator owns it.
type RegistryProvider interface {
	Publish(ctx context.Context, name, grpcAddress, microRPCAddress string) (withdraw func(context.Context) error, err error)
	Lookup(ctx context.Context, name, protocol string) ([]string, error)
}

type settings struct {
	name            string
	registry        RegistryProvider
	grpcAddress     string
	microRPCAddress string
	advertiseHost   string
	shutdownTimeout time.Duration
}

// Option configures a Kronos instance.
type Option func(*settings)

// Name sets the service name used for discovery and compatibility routing.
func Name(name string) Option {
	return func(config *settings) { config.name = name }
}

// Registry supplies service publication and discovery.
func Registry(registry RegistryProvider) Option {
	return func(config *settings) { config.registry = registry }
}

// GRPC configures a standard gRPC listener.
func GRPC(address string) Option {
	return func(config *settings) { config.grpcAddress = address }
}

// MicroRPC configures a go-micro v2 unary RPC listener.
func MicroRPC(address string) Option {
	return func(config *settings) { config.microRPCAddress = address }
}

// AdvertiseHost supplies the host that callers can reach on bound ports.
func AdvertiseHost(host string) Option {
	return func(config *settings) { config.advertiseHost = host }
}

// ShutdownTimeout configures the shared time budget for withdrawal and drain.
func ShutdownTimeout(timeout time.Duration) Option {
	return func(config *settings) { config.shutdownTimeout = timeout }
}

func applyOptions(options []Option) (settings, error) {
	config := settings{shutdownTimeout: 10 * time.Second}
	for _, option := range options {
		if option == nil {
			return settings{}, errors.New("Kronos options must be non-nil")
		}
		option(&config)
	}
	return config, nil
}
