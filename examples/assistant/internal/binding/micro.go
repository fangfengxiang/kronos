package binding

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"github.com/fangfengxiang/kronos/framework/localization"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/microv2"
	"github.com/fangfengxiang/kronos/infra/oteltrace"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/protobuf/proto"
)

const microV2Endpoint = "Assistant.Run"

var microV2Service = string(wire.File_kronos_assistant_v1_assistant_proto.Services().ByName("Assistant").FullName())

func NewMicroV2Server(listener net.Listener, run runHandler) rpc.Server {
	run = rpc.RecoverUnary(run)
	nativeServer := kratoshttp.NewServer(kratoshttp.Listener(listener), kratoshttp.Timeout(0))
	nativeServer.Handle("/", microv2.NewHandler(microV2Service, map[string]microv2.Unary{
		microV2Endpoint: func(ctx context.Context, payload []byte) ([]byte, error) {
			var input wire.RunRequest
			if err := proto.Unmarshal(payload, &input); err != nil {
				return nil, &microv2.Error{Code: http.StatusBadRequest, Reason: invalidRequestReason, Message: invalidRequestMessage}
			}
			response, err := run(withIncomingRequest(ctx), &input)
			if err != nil {
				return nil, toMicroV2Error(err)
			}
			return proto.Marshal(response)
		},
	}))
	return server{transport: nativeServer}
}

func NewMicroV2Client(ctx context.Context, endpoint string, defaultTimeout time.Duration) (*rpc.UnaryClient[wire.RunRequest, wire.RunResponse], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nativeClient, err := microv2.NewClient(endpoint)
	if err != nil {
		return nil, err
	}
	return newUnaryClient(func(ctx context.Context, input *wire.RunRequest) (*wire.RunResponse, error) {
		payload, err := proto.Marshal(input)
		if err != nil {
			return nil, err
		}
		metadata := microv2.RequestMetadata{AcceptLanguage: localization.AcceptLanguage(ctx)}
		oteltrace.Propagate(ctx, func(key, value string) {
			switch key {
			case "traceparent":
				metadata.TraceParent = value
			case "tracestate":
				metadata.TraceState = value
			}
		})
		decoded, err := nativeClient.CallWithRequestID(ctx, microV2Service, microV2Endpoint, payload, request.ID(ctx), metadata)
		if err != nil {
			var failure *microv2.Error
			if errors.As(err, &failure) {
				return nil, kratoserrors.New(failure.Code, failure.Reason, failure.Message)
			}
			return nil, err
		}
		var response wire.RunResponse
		if err := proto.Unmarshal(decoded, &response); err != nil {
			return nil, err
		}
		return &response, nil
	}, func() error {
		nativeClient.CloseIdleConnections()
		return nil
	}, defaultTimeout)
}

func toMicroV2Error(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	failure := kratoserrors.FromError(toWireError(err))
	return &microv2.Error{Code: int(failure.Code), Reason: failure.Reason, Message: failure.Message}
}
