package binding

import (
	"context"
	"errors"
	"net/http"
	"time"

	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"github.com/fangfengxiang/kronos/framework/rpc"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
)

func NewHTTPClient(ctx context.Context, endpoint string, defaultTimeout time.Duration) (*rpc.UnaryClient[wire.RunRequest, wire.RunResponse], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true}
	nativeClient, err := kratoshttp.NewClient(ctx, kratoshttp.WithEndpoint(endpoint), kratoshttp.WithTimeout(0),
		kratoshttp.WithTransport(transport), kratoshttp.WithMiddleware(propagateOutgoingRequest))
	if err != nil {
		return nil, fromWireError(err)
	}
	client := wire.NewAssistantHTTPClient(nativeClient)
	return newUnaryClient(func(ctx context.Context, request *wire.RunRequest) (*wire.RunResponse, error) {
		return client.Run(ctx, request)
	}, func() error {
		defer transport.CloseIdleConnections()
		return nativeClient.Close()
	}, defaultTimeout)
}

func NewGRPCClient(ctx context.Context, endpoint string, defaultTimeout time.Duration) (*rpc.UnaryClient[wire.RunRequest, wire.RunResponse], streamRunFunc, error) {
	connection, err := kratosgrpc.NewClient(ctx, kratosgrpc.WithEndpoint(endpoint), kratosgrpc.WithTimeout(0),
		kratosgrpc.WithMiddleware(propagateOutgoingRequest))
	if err != nil {
		return nil, nil, fromWireError(err)
	}
	client := wire.NewAssistantClient(connection)
	unary, err := newUnaryClient(func(ctx context.Context, request *wire.RunRequest) (*wire.RunResponse, error) {
		return client.Run(ctx, request)
	}, connection.Close, defaultTimeout)
	if err != nil {
		return nil, nil, err
	}
	return unary, newStreamRunner(client), nil
}

func newUnaryClient(invoke runHandler, closeConnection func() error, defaultTimeout time.Duration) (*rpc.UnaryClient[wire.RunRequest, wire.RunResponse], error) {
	closeClient := func() error { return fromWireError(closeConnection()) }
	client, err := rpc.NewUnaryClient(func(ctx context.Context, request *wire.RunRequest) (*wire.RunResponse, error) {
		response, err := invoke(ctx, request)
		return response, fromWireError(err)
	}, closeClient, defaultTimeout)
	if err != nil {
		return nil, errors.Join(err, closeClient())
	}
	return client, nil
}
