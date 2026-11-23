package kronos

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/platform/kronos/internal/transport"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

var (
	// ErrUnsupportedProtocol indicates an unknown wire protocol option.
	ErrUnsupportedProtocol = rpc.ErrUnsupportedProtocol
	// ErrClientClosed indicates that the client has released its connections.
	ErrClientClosed = rpc.ErrClientClosed
)

// Client resolves protocol addresses and owns the resulting connections.
type Client struct {
	config      settings
	ctx         context.Context
	cancel      context.CancelFunc
	closeClient func() error

	mu    sync.Mutex
	views map[string]*serviceView
	peers map[*peer]struct{}
}

// NewClient constructs an independently owned client using the supplied
// registry. It defaults to gRPC and discovers instances on the first call.
func NewClient(options ...Option) (*Client, error) {
	config, err := applyOptions(options)
	if err != nil {
		return nil, err
	}
	if _, err := rpc.ResolveProtocol(rpc.WithProtocol(config.protocol)); err != nil {
		return nil, err
	}
	if config.registry == nil {
		return nil, errors.New("Kronos client requires a registry")
	}
	if config.callTimeout <= 0 {
		return nil, errors.New("Kronos call timeout must be positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{
		config: config, ctx: ctx, cancel: cancel,
		views: make(map[string]*serviceView), peers: make(map[*peer]struct{}),
	}
	client.closeClient = sync.OnceValue(client.releaseConnections)
	return client, nil
}

// Close cancels pending client operations and closes its connections.
func (client *Client) Close() error {
	return client.closeClient()
}

func (client *Client) releaseConnections() error {
	client.cancel()
	client.mu.Lock()
	activePeers := client.peers
	client.views, client.peers = nil, nil
	client.mu.Unlock()
	var closeError error
	for peer := range activePeers {
		closeError = errors.Join(closeError, peer.connection.Close())
	}
	return closeError
}

// Invoke runs one unary Protobuf method. Typed generated clients call Invoke
// with the descriptors and Go messages from the same service contract.
func (client *Client) Invoke(ctx context.Context, method protoreflect.MethodDescriptor, request, response proto.Message) (callError error) {
	if client.ctx.Err() != nil {
		return ErrClientClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if method == nil || proto.MessageName(request) != method.Input().FullName() || proto.MessageName(response) != method.Output().FullName() ||
		!response.ProtoReflect().IsValid() {
		return &rpc.Error{Code: rpc.InvalidArgument, Reason: "INVALID_REQUEST", Message: "invalid RPC method or message type"}
	}
	if method.IsStreamingClient() || method.IsStreamingServer() {
		return rpc.ErrStreamingUnsupported
	}
	caller := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		ctx, cancel = context.WithCancel(ctx)
	} else {
		ctx, cancel = context.WithTimeout(ctx, client.config.callTimeout)
	}
	stopCancel := context.AfterFunc(client.ctx, cancel)
	defer func() {
		stopCancel()
		cancel()
	}()
	service := method.Parent().(protoreflect.ServiceDescriptor)
	serviceName := client.config.name
	if serviceName == "" {
		serviceName = string(service.FullName())
	}
	operation := transport.Operation{
		GRPCMethod:  "/" + string(service.FullName()) + "/" + string(method.Name()),
		MicroName:   serviceName,
		MicroMethod: string(service.Name()) + "." + string(method.Name()),
	}
	started := time.Now()
	defer func() {
		if callError != nil && ctx.Err() != nil {
			switch {
			case caller.Err() != nil:
				callError = caller.Err()
			case client.ctx.Err() != nil:
				callError = ErrClientClosed
			default:
				callError = ctx.Err()
			}
		}
		level := slog.LevelDebug
		if callError != nil {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "RPC client request completed", "event", "rpc_client_finished", "service", serviceName,
			"operation", operation.GRPCMethod, "protocol", client.config.protocol,
			"failed", callError != nil, "duration_us", time.Since(started).Microseconds())
	}()
	if err := client.resolve(ctx, serviceName); err != nil {
		return err
	}
	descriptorOptions, _ := method.Options().(*descriptorpb.MethodOptions)
	idempotent := descriptorOptions.GetIdempotencyLevel()
	attempts := 1
	if idempotent == descriptorpb.MethodOptions_IDEMPOTENT || idempotent == descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
		attempts++
	}
	var previousError error
	var previousAddress string
	for range attempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		target, err := client.acquire(ctx, serviceName, previousAddress)
		if err != nil {
			if previousError != nil {
				return previousError
			}
			return err
		}
		unavailable, err := target.connection.Invoke(ctx, operation, request, response)
		client.release(target, unavailable)
		if err == nil || !unavailable {
			return err
		}
		previousAddress, previousError = target.address, err
	}
	return previousError
}
