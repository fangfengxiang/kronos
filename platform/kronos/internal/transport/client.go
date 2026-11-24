package transport

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/fangfengxiang/kronos/framework/localization"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/microv2"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	kratosstatus "github.com/go-kratos/kratos/v3/transport/http/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type Operation struct {
	GRPCMethod  string
	MicroName   string
	MicroMethod string
	TraceParent string
	TraceState  string
}

type Client struct {
	Invoke func(context.Context, Operation, proto.Message, proto.Message) (unavailable bool, err error)
	Close  func() error
}

func Dial(ctx context.Context, protocol rpc.Protocol, address string) (Client, error) {
	switch protocol {
	case rpc.ProtocolGRPC:
		connection, err := kratosgrpc.NewClient(ctx, kratosgrpc.WithEndpoint(address), kratosgrpc.WithTimeout(0),
			kratosgrpc.WithOptions(grpc.WithDisableRetry()))
		if err != nil {
			return Client{}, err
		}
		return Client{
			Close: connection.Close,
			Invoke: func(ctx context.Context, op Operation, request, response proto.Message) (bool, error) {
				ctx = withGRPCMetadata(ctx, op)
				err := connection.Invoke(ctx, op.GRPCMethod, request, response)
				if err == nil {
					return false, nil
				}
				if ctxErr := ctx.Err(); ctxErr != nil {
					return false, ctxErr
				}
				failure := kratoserrors.FromError(err)
				return remoteFailure(int(failure.Code), failure.Reason, failure.Message)
			},
		}, nil
	case rpc.ProtocolMicroV2:
		return dialMicro(address)
	default:
		return Client{}, rpc.ErrUnsupportedProtocol
	}
}

func withGRPCMetadata(ctx context.Context, op Operation) context.Context {
	preferences := localization.AcceptLanguage(ctx)
	requestID := request.ID(ctx)
	if op.TraceParent == "" && len(preferences) == 0 && requestID == "" {
		return ctx
	}
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	if outgoing == nil {
		outgoing = make(metadata.MD)
	}
	if len(preferences) != 0 {
		outgoing.Set(localization.AcceptLanguageHeader, preferences...)
	}
	if requestID != "" {
		outgoing.Set(request.IDHeader, requestID)
	}
	if op.TraceParent != "" {
		outgoing.Set("traceparent", op.TraceParent)
		if op.TraceState == "" {
			outgoing.Delete("tracestate")
		} else {
			outgoing.Set("tracestate", op.TraceState)
		}
	}
	return metadata.NewOutgoingContext(ctx, outgoing)
}

func dialMicro(address string) (Client, error) {
	connection, err := microv2.NewClient(address)
	if err != nil {
		return Client{}, err
	}
	return Client{
		Close: func() error {
			connection.CloseIdleConnections()
			return nil
		},
		Invoke: func(ctx context.Context, op Operation, input, response proto.Message) (bool, error) {
			payload, err := proto.Marshal(input)
			if err != nil {
				return false, err
			}
			decoded, err := connection.CallWithRequestID(ctx, op.MicroName, op.MicroMethod, payload, request.ID(ctx), microv2.RequestMetadata{
				TraceParent: op.TraceParent, TraceState: op.TraceState, AcceptLanguage: localization.AcceptLanguage(ctx),
			})
			if err == nil {
				return false, proto.Unmarshal(decoded, response)
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return false, ctxErr
			}
			var remote *microv2.Error
			if errors.As(err, &remote) {
				return remoteFailure(remote.Code, remote.Reason, remote.Message)
			}
			var networkError *url.Error
			if errors.As(err, &networkError) {
				return remoteFailure(http.StatusServiceUnavailable, "", "")
			}
			return remoteFailure(http.StatusInternalServerError, "", "")
		},
	}, nil
}

func remoteFailure(status int, reason, message string) (bool, error) {
	switch status {
	case kratosstatus.ClientClosed:
		return false, context.Canceled
	case http.StatusGatewayTimeout:
		return false, context.DeadlineExceeded
	case http.StatusBadRequest:
		return false, &rpc.Error{Code: rpc.InvalidArgument, Reason: reason, Message: message}
	case http.StatusServiceUnavailable:
		if reason == "" {
			reason, message = rpc.ServiceUnavailableReason, "rpc instance is unavailable"
		}
		return reason == rpc.ServiceUnavailableReason, &rpc.Error{Code: rpc.Unavailable, Reason: reason, Message: message}
	default:
		if reason == "" {
			reason, message = "INTERNAL", "RPC failed"
		}
		return false, &rpc.Error{Code: rpc.Internal, Reason: reason, Message: message}
	}
}
