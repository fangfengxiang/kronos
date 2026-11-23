package binding

import (
	"context"
	"errors"
	"net"
	"time"

	wire "github.com/fangfengxiang/kronos/examples/textbackend/internal/wire/kronos/textbackend/v1"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"google.golang.org/grpc"
)

type generateFunc = func(context.Context, *wire.GenerateRequest) (*wire.GenerateResponse, error)

type backendService struct {
	wire.UnimplementedTextBackendServiceServer
	generate generateFunc
}

func (service backendService) Generate(ctx context.Context, input *wire.GenerateRequest) (*wire.GenerateResponse, error) {
	if incoming, ok := transport.FromServerContext(ctx); ok {
		ctx = request.WithIncomingID(ctx, incoming.RequestHeader().Values(request.IDHeader)...)
	}
	response, err := service.generate(ctx, input)
	return response, toWireError(err)
}

type server struct{ rpc.Server }

func NewServer(listener net.Listener, generate generateFunc) rpc.Server {
	native := kratosgrpc.NewServer(kratosgrpc.Listener(listener), kratosgrpc.Timeout(0))
	wire.RegisterTextBackendServiceServer(native, backendService{generate: generate})
	return server{Server: native}
}

func NewClient(ctx context.Context, endpoint string, defaultTimeout time.Duration) (*rpc.UnaryClient[wire.GenerateRequest, wire.GenerateResponse], error) {
	connection, err := kratosgrpc.NewClient(ctx, kratosgrpc.WithEndpoint(endpoint), kratosgrpc.WithTimeout(0),
		kratosgrpc.WithMiddleware(propagateRequestID),
		// A fixed address uses the standard policy independently of Kratos's global selector.
		kratosgrpc.WithOptions(grpc.WithDisableServiceConfig(), grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"pick_first":{}}]}`)))
	if err != nil {
		return nil, fromWireError(err)
	}
	native := wire.NewTextBackendServiceClient(connection)
	closeConnection := func() error { return fromWireError(connection.Close()) }
	client, err := rpc.NewUnaryClient(func(ctx context.Context, input *wire.GenerateRequest) (*wire.GenerateResponse, error) {
		response, err := native.Generate(ctx, input)
		return response, fromWireError(err)
	}, closeConnection, defaultTimeout)
	if err != nil {
		return nil, errors.Join(err, closeConnection())
	}
	return client, nil
}

func propagateRequestID(next middleware.Handler) middleware.Handler {
	return func(ctx context.Context, input any) (any, error) {
		if outgoing, ok := transport.FromClientContext(ctx); ok {
			if id := request.ID(ctx); id != "" {
				outgoing.RequestHeader().Set(request.IDHeader, id)
			}
		}
		return next(ctx, input)
	}
}
