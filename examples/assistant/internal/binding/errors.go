package binding

import (
	"context"
	"errors"
	"net/http"

	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"github.com/fangfengxiang/kronos/framework/rpc"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	kratosstatus "github.com/go-kratos/kratos/v3/transport/http/status"
)

const (
	invalidRequestReason  = "INVALID_REQUEST"
	invalidRequestMessage = "invalid request body"
	runFailedMessage      = "assistant run failed"
)

// PublicError applies Assistant's stable error contract at a service boundary.
func PublicError(err error) error {
	return fromWireError(toWireError(err))
}

func toWireError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return kratoserrors.New(kratosstatus.ClientClosed, "CANCELED", context.Canceled.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return kratoserrors.New(http.StatusGatewayTimeout, "DEADLINE_EXCEEDED", context.DeadlineExceeded.Error())
	}

	invalidPromptReason := wire.ErrorReason_ERROR_REASON_INVALID_PROMPT.String()
	var publicError *rpc.Error
	if errors.As(err, &publicError) && publicError != nil {
		switch {
		case publicError.Code == rpc.InvalidArgument && publicError.Reason == invalidPromptReason:
			return kratoserrors.BadRequest(invalidPromptReason, publicError.Message)
		case publicError.Code == rpc.Unavailable && publicError.Reason == rpc.ServiceUnavailableReason:
			return kratoserrors.ServiceUnavailable(rpc.ServiceUnavailableReason, publicError.Message)
		}
	}

	return kratoserrors.InternalServer(wire.ErrorReason_ERROR_REASON_RUN_FAILED.String(), runFailedMessage)
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

	transportError := kratoserrors.FromError(err)
	switch int(transportError.Code) {
	case kratosstatus.ClientClosed:
		return context.Canceled
	case http.StatusGatewayTimeout:
		return context.DeadlineExceeded
	case http.StatusBadRequest:
		if transportError.Reason == wire.ErrorReason_ERROR_REASON_INVALID_PROMPT.String() || transportError.Reason == invalidRequestReason {
			return &rpc.Error{Code: rpc.InvalidArgument, Reason: transportError.Reason, Message: transportError.Message}
		}
	case http.StatusServiceUnavailable:
		if transportError.Reason == rpc.ServiceUnavailableReason {
			return &rpc.Error{Code: rpc.Unavailable, Reason: transportError.Reason, Message: transportError.Message}
		}
	}

	return &rpc.Error{Code: rpc.Internal, Reason: wire.ErrorReason_ERROR_REASON_RUN_FAILED.String(), Message: runFailedMessage}
}
