package rpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// ErrHandlerPanic indicates that an RPC handler panicked during the call.
var ErrHandlerPanic = errors.New("RPC handler panicked")

// RecoverUnary wraps a unary handler with same-goroutine panic recovery.
// A nil handler remains nil for bindings that use it to disable a method.
func RecoverUnary[Request, Response any](handler func(context.Context, *Request) (*Response, error)) func(context.Context, *Request) (*Response, error) {
	return RecoverCall(handler)
}

// RecoverCall protects a unary invocation that accepts values or interfaces.
func RecoverCall[Request, Response any](handler func(context.Context, Request) (Response, error)) func(context.Context, Request) (Response, error) {
	if handler == nil {
		return nil
	}
	return func(ctx context.Context, request Request) (_ Response, err error) {
		defer recoverHandler(ctx, &err)
		return handler(ctx, request)
	}
}

// RecoverStream wraps a streaming handler. A panic leaves the transport
// responsible for closing the stream and mapping ErrHandlerPanic.
func RecoverStream[Request, Event any](handler func(context.Context, *Request, func(Event) error) error) func(context.Context, *Request, func(Event) error) error {
	if handler == nil {
		return nil
	}
	return func(ctx context.Context, request *Request, emit func(Event) error) (err error) {
		defer recoverHandler(ctx, &err)
		return handler(ctx, request, emit)
	}
}

func recoverHandler(ctx context.Context, callError *error) {
	if value := recover(); value != nil {
		*callError = ErrHandlerPanic
		reportHandlerPanic(ctx, value)
	}
}

func reportHandlerPanic(ctx context.Context, value any) {
	// A diagnostic logger may call application code while handling the record.
	defer func() { _ = recover() }()
	slog.ErrorContext(ctx, "RPC handler panicked", "panic_type", fmt.Sprintf("%T", value), "stack", string(debug.Stack()))
}
