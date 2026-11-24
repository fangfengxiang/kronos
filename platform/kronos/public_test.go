package kronos_test

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/fangfengxiang/kronos/platform/kronos"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Compile these assertions as an application consuming the public package.
var (
	_ func(string) kronos.Option                                                 = kronos.AdvertiseHost
	_ func(time.Duration) kronos.Option                                          = kronos.ShutdownTimeout
	_ func(...kronos.Option) (*kronos.App, error)                                = kronos.New
	_ func(...kronos.Option) (*kronos.Client, error)                             = kronos.NewClient
	_ func(string) kronos.Option                                                 = kronos.Name
	_ func(kronos.RegistryProvider) kronos.Option                                = kronos.Registry
	_ func(string) kronos.Option                                                 = kronos.GRPC
	_ func(string) kronos.Option                                                 = kronos.MicroRPC
	_ func(string) kronos.Option                                                 = kronos.HTTP
	_ func(string) kronos.Option                                                 = kronos.SSE
	_ func(kronos.WireProtocol) kronos.Option                                    = kronos.Protocol
	_ func(time.Duration) kronos.Option                                          = kronos.CallTimeout
	_ func(kronos.RPCMetrics) kronos.Option                                      = kronos.Metrics
	_ func(kronos.RPCTracer) kronos.Option                                       = kronos.Tracing
	_ func(kronos.ObservabilityConfig) kronos.Option                             = kronos.Observability
	_ func(string, http.Handler) kronos.Option                                   = kronos.Diagnostics
	_ func(protoreflect.ServiceDescriptor, ...kronos.UnaryMethod) kronos.Service = kronos.Bind
	_ func(protoreflect.Name, *emptypb.Empty, *emptypb.Empty,
		func(context.Context, *emptypb.Empty) (*emptypb.Empty, error)) kronos.UnaryMethod = kronos.Unary[*emptypb.Empty, *emptypb.Empty]
	_ func(protoreflect.Name, *emptypb.Empty, *emptypb.Empty,
		func(context.Context, *emptypb.Empty, func(*emptypb.Empty) error) error) kronos.ServerStreamMethod = kronos.ServerStream[*emptypb.Empty, *emptypb.Empty]
	_ = &kronos.Error{Code: kronos.InvalidArgument, Reason: "INVALID_REQUEST", Message: "invalid argument"}
	_ interface {
		Register(kronos.Service) error
		Run(context.Context) error
		RunWithReload(context.Context, kronos.ReloadConfig, <-chan os.Signal) error
	} = (*kronos.App)(nil)
	_ interface {
		Invoke(context.Context, protoreflect.MethodDescriptor, proto.Message, proto.Message) error
		Close() error
	} = (*kronos.Client)(nil)
)
