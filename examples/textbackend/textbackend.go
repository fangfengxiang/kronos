// Package textbackend exposes the contract of the reference text provider.
package textbackend

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend/internal/binding"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

// DefaultServiceName identifies the reference text provider in a registry.
const DefaultServiceName = "textbackend"

// NewGRPCServer serves the given implementation on the supplied listener.
func NewGRPCServer(listener net.Listener, service TextBackendServiceUnaryService) (rpc.Server, error) {
	if listener == nil || service == nil {
		return nil, errors.New("text backend requires a listener and an implementation")
	}
	return binding.NewServer(listener, service.Generate), nil
}

// Client owns a reusable connection to the text provider.
type Client struct {
	unary *rpc.UnaryClient[GenerateRequest, GenerateResponse]
}

// EndpointDiscovery supplies complete snapshots of reachable gRPC endpoints.
// Watch must deliver an initial snapshot and return promptly when ctx is canceled.
type EndpointDiscovery interface {
	Watch(ctx context.Context, serviceName string, observe func([]string)) error
}

// NewGRPCClient creates a plaintext client for a host:port endpoint and starts
// connecting asynchronously. defaultTimeout applies when a call has no deadline.
func NewGRPCClient(ctx context.Context, endpoint string, defaultTimeout time.Duration) (*Client, error) {
	if host, port, err := net.SplitHostPort(endpoint); err != nil || host == "" || port == "" {
		return nil, fmt.Errorf("text backend address %q must be host:port", endpoint)
	}
	unary, err := binding.NewClient(ctx, endpoint, defaultTimeout)
	if err != nil {
		return nil, err
	}
	return &Client{unary: unary}, nil
}

// NewDiscoveredGRPCClient follows a service's direct plaintext endpoints while
// the connection is active. defaultTimeout applies to calls without a deadline.
func NewDiscoveredGRPCClient(ctx context.Context, serviceName string, defaultTimeout time.Duration, discovery EndpointDiscovery) (*Client, error) {
	if discovery == nil || serviceName == "" || serviceName == "." || serviceName == ".." || url.PathEscape(serviceName) != serviceName {
		return nil, errors.New("text backend discovery requires a source and a service name valid in a URL path segment")
	}
	unary, err := binding.NewDiscoveredClient(ctx, serviceName, defaultTimeout, discovery.Watch)
	if err != nil {
		return nil, err
	}
	return &Client{unary: unary}, nil
}

// Generate asks the backend to produce text for the prompt.
func (client *Client) Generate(ctx context.Context, request *GenerateRequest) (*GenerateResponse, error) {
	return client.unary.Call(ctx, request)
}

// Close releases the connection. It is safe to call repeatedly.
func (client *Client) Close() error {
	return client.unary.Close()
}
