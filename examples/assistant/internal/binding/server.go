package binding

import (
	"context"
	"net"
	"net/http"
	"time"

	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"github.com/fangfengxiang/kronos/framework/rpc"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
)

const sseWriteTimeout = 15 * time.Second

type runHandler = func(context.Context, *wire.RunRequest) (*wire.RunResponse, error)
type streamHandler = func(context.Context, *wire.StreamRunRequest, func(string) error) error

type service struct {
	wire.UnimplementedAssistantServer
	run    runHandler
	stream streamHandler
}

func (service service) StreamRun(request *wire.StreamRunRequest, stream wire.Assistant_StreamRunServer) error {
	if service.stream == nil {
		return service.UnimplementedAssistantServer.StreamRun(request, stream)
	}
	ctx := stream.Context()
	if requestContext, ok := ctx.Value(sseRequestContextKey{}).(context.Context); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		stopCancellation := context.AfterFunc(requestContext, cancel)
		defer stopCancellation()
		if requestContext.Err() != nil {
			cancel()
		}
	}
	writeDeadline, _ := stream.(interface{ SetWriteDeadline(time.Time) error })
	send := func(event *wire.StreamRunResponse) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if writeDeadline != nil {
			_ = writeDeadline.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
			defer writeDeadline.SetWriteDeadline(time.Time{})
		}
		return stream.Send(event)
	}
	if err := service.stream(withIncomingLanguage(ctx), request, func(delta string) error {
		return send(&wire.StreamRunResponse{Event: &wire.StreamRunResponse_TextDelta{TextDelta: delta}})
	}); err != nil {
		if writeDeadline != nil {
			// Kratos may write a terminal SSE error after this handler returns.
			_ = writeDeadline.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
		}
		return toWireError(err)
	}
	return toWireError(send(&wire.StreamRunResponse{Event: &wire.StreamRunResponse_Completed{Completed: &wire.RunCompleted{}}}))
}

func (service service) Run(ctx context.Context, request *wire.RunRequest) (*wire.RunResponse, error) {
	response, err := service.run(withIncomingLanguage(ctx), request)
	return response, toWireError(err)
}

type server struct {
	transport rpc.Server
}

func (server server) Start(ctx context.Context) error {
	return server.transport.Start(ctx)
}

func (server server) Stop(ctx context.Context) error {
	return server.transport.Stop(ctx)
}

func NewHTTPServer(listener net.Listener, run runHandler, probes http.Handler) rpc.Server {
	nativeServer := kratoshttp.NewServer(kratoshttp.Listener(listener), kratoshttp.ErrorEncoder(encodeHTTPError), kratoshttp.Filter(onlyStreamRoute(false)))
	wire.RegisterAssistantHTTPServer(nativeServer, service{run: run})
	if probes != nil {
		nativeServer.Handle("/livez", probes)
		nativeServer.Handle("/readyz", probes)
	}
	return server{transport: nativeServer}
}

func NewGRPCServer(listener net.Listener, run runHandler, stream streamHandler) rpc.Server {
	nativeServer := kratosgrpc.NewServer(kratosgrpc.Listener(listener))
	wire.RegisterAssistantServer(nativeServer, service{run: run, stream: stream})
	return server{transport: nativeServer}
}

func NewSSEServer(listener net.Listener, stream streamHandler) rpc.Server {
	nativeServer := kratoshttp.NewServer(kratoshttp.Listener(listener), kratoshttp.Timeout(0),
		kratoshttp.ErrorEncoder(encodeHTTPError), kratoshttp.Filter(onlyStreamRoute(true)))
	wire.RegisterAssistantHTTPServer(nativeServer, service{stream: stream})
	return server{transport: nativeServer}
}

type sseRequestContextKey struct{}

func onlyStreamRoute(sse bool) kratoshttp.FilterFunc {
	method := wire.File_kronos_assistant_v1_assistant_proto.Services().ByName("Assistant").Methods().ByName("StreamRun")
	rule := proto.GetExtension(method.Options(), annotations.E_Http).(*annotations.HttpRule)
	streamPath := rule.GetPost()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if (request.URL.Path == streamPath) != sse {
				http.NotFound(response, request)
				return
			}
			if sse {
				requestContext := request.Context()
				request = request.Clone(context.WithValue(requestContext, sseRequestContextKey{}, requestContext))
				request.Header.Set("Accept", "application/protojson")
			}
			next.ServeHTTP(response, request)
		})
	}
}

func encodeHTTPError(response http.ResponseWriter, request *http.Request, err error) {
	if transportError, ok := err.(*kratoserrors.Error); !ok {
		err = toWireError(err)
	} else if transportError.Reason == "CODEC" {
		err = kratoserrors.BadRequest(invalidRequestReason, invalidRequestMessage)
	}
	kratoshttp.DefaultErrorEncoder(response, request, err)
}
