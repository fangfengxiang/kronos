// Package assistant exposes the reference service and its transport bindings.
package assistant

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant/internal/binding"
	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

type (
	RunRequest       = wire.RunRequest
	RunResponse      = wire.RunResponse
	ErrorReason      = wire.ErrorReason
	StreamRunRequest = wire.StreamRunRequest
)

const (
	ErrorReasonInvalidPrompt = wire.ErrorReason_ERROR_REASON_INVALID_PROMPT
	ErrorReasonRunFailed     = wire.ErrorReason_ERROR_REASON_RUN_FAILED
)

// Service implements the business operation shared by the unary transports.
type Service interface {
	Run(context.Context, *RunRequest) (*RunResponse, error)
}

// StreamService produces ordered text deltas. Call emit sequentially while
// StreamRun is active. A nil error completes the stream.
type StreamService interface {
	StreamRun(ctx context.Context, request *StreamRunRequest, emit func(string) error) error
}

// NewHTTPServer binds service to a caller-provided HTTP listener.
func NewHTTPServer(listener net.Listener, service Service) rpc.Server {
	return binding.NewHTTPServer(listener, service.Run, nil)
}

// NewHTTPServerWithProbes also mounts probes at /livez and /readyz.
func NewHTTPServerWithProbes(listener net.Listener, service Service, probes http.Handler) rpc.Server {
	return binding.NewHTTPServer(listener, service.Run, probes)
}

// NewGRPCServer binds service to a caller-provided plaintext gRPC listener.
func NewGRPCServer(listener net.Listener, service Service) rpc.Server {
	return binding.NewGRPCServer(listener, service.Run, nil)
}

// NewGRPCServerWithStream serves one unary and one streaming implementation.
func NewGRPCServerWithStream(listener net.Listener, service Service, streams StreamService) rpc.Server {
	return binding.NewGRPCServer(listener, service.Run, streams.StreamRun)
}

// NewSSEServer serves StreamRun on a dedicated HTTP listener.
func NewSSEServer(listener net.Listener, streams StreamService) rpc.Server {
	return binding.NewSSEServer(listener, streams.StreamRun)
}

// NewMicroV2Server serves unary calls from a go-micro v2 client.
func NewMicroV2Server(listener net.Listener, service Service) rpc.Server {
	return binding.NewMicroV2Server(listener, service.Run)
}

// Client invokes the reference service through its selected transport.
type Client struct {
	unary *rpc.UnaryClient[RunRequest, RunResponse]
}

// NewHTTPClient connects to an HTTP endpoint. DefaultTimeout applies to calls
// whose contexts have no deadline.
func NewHTTPClient(ctx context.Context, endpoint string, defaultTimeout time.Duration) (*Client, error) {
	unary, err := binding.NewHTTPClient(ctx, endpoint, defaultTimeout)
	if err != nil {
		return nil, err
	}
	return &Client{unary: unary}, nil
}

// NewGRPCClient connects to a plaintext gRPC endpoint. DefaultTimeout applies
// to calls whose contexts have no deadline.
func NewGRPCClient(ctx context.Context, endpoint string, defaultTimeout time.Duration) (*Client, error) {
	unary, err := binding.NewGRPCClient(ctx, endpoint, defaultTimeout)
	if err != nil {
		return nil, err
	}
	return &Client{unary: unary}, nil
}

// NewRPCClient connects over standard gRPC by default. Use rpc.WithProtocol to
// select the go-micro v2 unary protocol for a compatibility endpoint.
func NewRPCClient(ctx context.Context, endpoint string, defaultTimeout time.Duration, options ...rpc.ClientOption) (*Client, error) {
	protocol, err := rpc.ResolveProtocol(options...)
	if err != nil {
		return nil, err
	}
	if protocol == rpc.ProtocolGRPC {
		return NewGRPCClient(ctx, endpoint, defaultTimeout)
	}
	unary, err := binding.NewMicroV2Client(ctx, endpoint, defaultTimeout)
	if err != nil {
		return nil, err
	}
	return &Client{unary: unary}, nil
}

// Run invokes the reference service's unary operation.
func (client *Client) Run(ctx context.Context, request *RunRequest) (*RunResponse, error) {
	return client.unary.Call(ctx, request)
}

// Close releases the client's transport resources.
func (client *Client) Close() error {
	return client.unary.Close()
}
