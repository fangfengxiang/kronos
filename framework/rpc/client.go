// Package rpc defines the contracts shared by Kronos RPC implementations.
package rpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrClientClosed indicates that a call was attempted after Close began.
var ErrClientClosed = errors.New("rpc client is closed")

// UnaryClient owns a typed invocation and the connection used by that invocation.
// It is safe for concurrent use if the invocation and connection support it.
type UnaryClient[Request, Response any] struct {
	invoke         func(context.Context, *Request) (*Response, error)
	close          func() error
	closed         atomic.Bool
	defaultTimeout time.Duration
}

// NewUnaryClient takes ownership of closeConnection on success. The invocation
// must translate transport failures to the public RPC error contract.
func NewUnaryClient[Request, Response any](
	invoke func(context.Context, *Request) (*Response, error),
	closeConnection func() error,
	defaultTimeout time.Duration,
) (*UnaryClient[Request, Response], error) {
	if invoke == nil || closeConnection == nil {
		return nil, errors.New("rpc client requires invocation and close functions")
	}
	if defaultTimeout < 0 {
		return nil, errors.New("rpc client default timeout must not be negative")
	}
	client := &UnaryClient[Request, Response]{invoke: invoke, defaultTimeout: defaultTimeout}
	client.close = sync.OnceValue(func() error {
		client.closed.Store(true)
		return closeConnection()
	})
	return client, nil
}

// Call preserves the caller's deadline. It applies a configured default only
// when the caller supplied none.
func (client *UnaryClient[Request, Response]) Call(ctx context.Context, request *Request) (*Response, error) {
	var response *Response
	err := client.Do(ctx, func(callContext context.Context) error {
		var err error
		response, err = client.invoke(callContext, request)
		return err
	})
	return response, err
}

// Do runs an operation on the same connection with the client's deadline and
// close checks. A default deadline applies only when ctx has no deadline.
func (client *UnaryClient[Request, Response]) Do(ctx context.Context, operation func(context.Context) error) error {
	if client.closed.Load() {
		return ErrClientClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if operation == nil {
		return errors.New("rpc client requires an operation")
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && client.defaultTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, client.defaultTimeout)
		defer cancel()
	}
	return operation(ctx)
}

// Close releases the connection once and returns the same result on later calls.
// The transport controls the outcome of calls already in flight.
func (client *UnaryClient[Request, Response]) Close() error {
	return client.close()
}
