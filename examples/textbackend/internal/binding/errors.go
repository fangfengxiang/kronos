package binding

import (
	"context"
	"errors"

	wire "github.com/fangfengxiang/kronos/examples/textbackend/internal/wire/kronos/textbackend/v1"
	"github.com/fangfengxiang/kronos/framework/rpc"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const generationFailedMessage = "text generation failed"

func toWireError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	var failure *rpc.Error
	if errors.As(err, &failure) && failure != nil && failure.Code == rpc.InvalidArgument &&
		failure.Reason == wire.ErrorReason_ERROR_REASON_INVALID_PROMPT.String() {
		return kratoserrors.BadRequest(failure.Reason, failure.Message)
	}
	return kratoserrors.InternalServer(wire.ErrorReason_ERROR_REASON_GENERATION_FAILED.String(), generationFailedMessage)
}

func fromWireError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	}
	switch status.Code(err) {
	case codes.Canceled:
		return context.Canceled
	case codes.DeadlineExceeded:
		return context.DeadlineExceeded
	case codes.Unavailable:
		return &rpc.Error{Code: rpc.Unavailable, Reason: rpc.ServiceUnavailableReason, Message: "text backend is unavailable"}
	case codes.InvalidArgument:
		failure := kratoserrors.FromError(err)
		if failure.Reason == wire.ErrorReason_ERROR_REASON_INVALID_PROMPT.String() {
			return &rpc.Error{Code: rpc.InvalidArgument, Reason: failure.Reason, Message: failure.Message}
		}
	}
	return &rpc.Error{Code: rpc.Internal, Reason: wire.ErrorReason_ERROR_REASON_GENERATION_FAILED.String(), Message: generationFailedMessage}
}
