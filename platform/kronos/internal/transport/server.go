package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/fangfengxiang/kronos/framework/app"
	"github.com/fangfengxiang/kronos/framework/localization"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/microv2"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	kratostransport "github.com/go-kratos/kratos/v3/transport"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	kratosstatus "github.com/go-kratos/kratos/v3/transport/http/status"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type Service struct {
	Name      string
	ShortName string
	Filename  string
	Methods   []Method
}

type Method struct {
	Name       string
	NewRequest func() proto.Message
	Invoke     func(context.Context, proto.Message) (proto.Message, error)
}

func GRPCServer(listener net.Listener, services []Service) (app.Server, error) {
	server := kratosgrpc.NewServer(kratosgrpc.Listener(listener), kratosgrpc.Timeout(0))
	for _, service := range services {
		if _, exists := server.GetServiceInfo()[service.Name]; exists {
			_ = server.Stop(context.Background())
			return nil, fmt.Errorf("gRPC already registers %q", service.Name)
		}
		description := grpc.ServiceDesc{
			ServiceName: service.Name, HandlerType: (*any)(nil), Metadata: service.Filename,
			Methods: make([]grpc.MethodDesc, 0, len(service.Methods)),
		}
		for _, method := range service.Methods {
			description.Methods = append(description.Methods, grpcMethod(service.Name, method))
		}
		server.RegisterService(&description, struct{}{})
	}
	return server, nil
}

func grpcMethod(serviceName string, method Method) grpc.MethodDesc {
	fullMethod := "/" + serviceName + "/" + method.Name
	return grpc.MethodDesc{MethodName: method.Name, Handler: func(
		service any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor,
	) (any, error) {
		request := method.NewRequest()
		if err := decode(request); err != nil {
			return nil, err
		}
		invoke := func(ctx context.Context, request any) (any, error) {
			response, err := method.Invoke(withIncomingRequest(ctx), request.(proto.Message))
			if err == nil {
				return response, nil
			}
			code, reason, message := failure(err)
			return nil, kratoserrors.New(code, reason, message)
		}
		if interceptor == nil {
			return invoke(ctx, request)
		}
		info := &grpc.UnaryServerInfo{Server: service, FullMethod: fullMethod}
		return interceptor(ctx, request, info, invoke)
	}}
}

func MicroRPCServer(listener net.Listener, serviceName string, services []Service) (app.Server, error) {
	endpoints := make(map[string]microv2.Unary)
	for _, service := range services {
		for _, method := range service.Methods {
			name := service.ShortName + "." + method.Name
			if _, exists := endpoints[name]; exists {
				return nil, fmt.Errorf("go-micro v2 endpoint %q is already registered", name)
			}
			endpoints[name] = func(ctx context.Context, payload []byte) ([]byte, error) {
				request := method.NewRequest()
				if err := proto.Unmarshal(payload, request); err != nil {
					return nil, &microv2.Error{Code: http.StatusBadRequest, Reason: "INVALID_REQUEST", Message: "invalid Protobuf request"}
				}
				response, err := method.Invoke(withIncomingRequest(ctx), request)
				if err == nil {
					return proto.Marshal(response)
				}
				code, reason, message := failure(err)
				return nil, &microv2.Error{Code: code, Reason: reason, Message: message}
			}
		}
	}
	server := newHTTPServer(listener)
	server.Handle("/", microv2.NewHandler(serviceName, endpoints))
	return server, nil
}

func HTTPServer(listener net.Listener, handler http.Handler) app.Server {
	server := newHTTPServer(listener)
	server.HandlePrefix("/", handler)
	return server
}

func newHTTPServer(listener net.Listener) *kratoshttp.Server {
	return kratoshttp.NewServer(kratoshttp.Listener(listener), kratoshttp.Timeout(0))
}

func withIncomingRequest(ctx context.Context) context.Context {
	if server, ok := kratostransport.FromServerContext(ctx); ok {
		headers := server.RequestHeader()
		ctx = request.WithIncomingID(ctx, headers.Values(request.IDHeader)...)
		return localization.WithIncomingAcceptLanguage(ctx, headers.Values(localization.AcceptLanguageHeader)...)
	}
	return localization.WithIncomingAcceptLanguage(request.WithIncomingID(ctx))
}

func failure(err error) (int, string, string) {
	switch {
	case errors.Is(err, context.Canceled):
		return kratosstatus.ClientClosed, "CANCELED", context.Canceled.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "DEADLINE_EXCEEDED", context.DeadlineExceeded.Error()
	}
	var public *rpc.Error
	if errors.As(err, &public) && public != nil {
		switch public.Code {
		case rpc.InvalidArgument:
			return http.StatusBadRequest, public.Reason, public.Message
		case rpc.Unavailable:
			return http.StatusServiceUnavailable, public.Reason, public.Message
		case rpc.Internal:
			return http.StatusInternalServerError, public.Reason, public.Message
		}
	}
	return http.StatusInternalServerError, "INTERNAL", "RPC failed"
}
