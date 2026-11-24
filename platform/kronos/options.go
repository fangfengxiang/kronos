// Package kronos assembles services and transports from public Protobuf
// contracts.
package kronos

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/fangfengxiang/kronos/framework/rpc"
)

// RegistryProvider publishes and resolves instances within the supplied
// contexts. Its creator owns it. A provider can declare supported transports
// with SupportsProtocol(string) bool.
type RegistryProvider interface {
	Publish(ctx context.Context, name, grpcAddress, microRPCAddress string) (withdraw func(context.Context) error, err error)
	Lookup(ctx context.Context, name, protocol string) ([]string, error)
}

func requireRegistryProtocol(registry RegistryProvider, protocol WireProtocol) error {
	if capability, ok := registry.(interface{ SupportsProtocol(string) bool }); ok && !capability.SupportsProtocol(string(protocol)) {
		return fmt.Errorf("%w: registry cannot use %s", ErrUnsupportedProtocol, protocol)
	}
	return nil
}

// RPCMetrics observes one completed unary call at a client or server boundary.
// BeginRPC may return nil for a method the observer does not record.
type RPCMetrics interface {
	BeginRPC(operation, protocol, side string) func(failed bool, duration time.Duration)
}

// RPCTracer observes a unary call and propagates its W3C trace context.
// StartRPC preserves the caller's deadline and cancellation in its context.
type RPCTracer interface {
	StartRPC(ctx context.Context, operation, protocol, side string) (context.Context, func(status string))
	Propagate(ctx context.Context, set func(string, string))
}

type settings struct {
	name            string
	registry        RegistryProvider
	grpcAddress     string
	microRPCAddress string
	diagnostics     string
	scrape          http.Handler
	metrics         RPCMetrics
	tracer          RPCTracer
	advertiseHost   string
	shutdownTimeout time.Duration
	protocol        rpc.Protocol
	callTimeout     time.Duration
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

// Metrics borrows an optional unary RPC observer for the app or client.
func Metrics(metrics RPCMetrics) Option {
	return func(config *settings) { config.metrics = metrics }
}

// Tracing borrows an optional tracer for the app or client.
func Tracing(tracer RPCTracer) Option {
	return func(config *settings) { config.tracer = tracer }
}

// Diagnostics configures an HTTP listener for liveness, readiness and an
// optional /metrics handler. It is never published for service discovery.
func Diagnostics(address string, scrape http.Handler) Option {
	return func(config *settings) {
		config.diagnostics = address
		config.scrape = scrape
	}
}

// AdvertiseHost supplies the host that callers can reach on bound ports.
func AdvertiseHost(host string) Option {
	return func(config *settings) { config.advertiseHost = host }
}

// ShutdownTimeout configures the shared time budget for withdrawal and drain.
func ShutdownTimeout(timeout time.Duration) Option {
	return func(config *settings) { config.shutdownTimeout = timeout }
}

const (
	ProtocolGRPC    = rpc.ProtocolGRPC
	ProtocolMicroV2 = rpc.ProtocolMicroV2
)

// WireProtocol identifies a supported or configured client wire protocol.
type WireProtocol = rpc.Protocol

// Protocol selects the client's wire protocol. The default is standard gRPC.
func Protocol(protocol WireProtocol) Option {
	return func(config *settings) { config.protocol = protocol }
}

// CallTimeout sets the deadline for a call without a caller-supplied deadline.
func CallTimeout(timeout time.Duration) Option {
	return func(config *settings) { config.callTimeout = timeout }
}

func applyOptions(options []Option) (settings, error) {
	config := settings{shutdownTimeout: 10 * time.Second, protocol: ProtocolGRPC, callTimeout: 3 * time.Second}
	for _, option := range options {
		if option == nil {
			return settings{}, errors.New("Kronos options must be non-nil")
		}
		option(&config)
	}
	return config, nil
}
