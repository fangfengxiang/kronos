package rpc_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/framework/rpc"
)

func TestUnaryClientUsesDefaultOnlyWithoutCallerDeadline(t *testing.T) {
	const defaultTimeout = time.Minute
	var invocationContext context.Context
	client, err := rpc.NewUnaryClient(
		func(ctx context.Context, request *string) (*string, error) {
			invocationContext = ctx
			return request, nil
		},
		func() error { return nil },
		defaultTimeout,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request := "example"
	beforeCall := time.Now()
	if _, err := client.Call(t.Context(), &request); err != nil {
		t.Fatal(err)
	}
	if deadline, ok := invocationContext.Deadline(); !ok || deadline.Before(beforeCall.Add(defaultTimeout)) || deadline.After(time.Now().Add(defaultTimeout)) {
		t.Errorf("default deadline = %v, present = %t", deadline, ok)
	}
	if !errors.Is(invocationContext.Err(), context.Canceled) {
		t.Errorf("default timeout context was not released after return: %v", invocationContext.Err())
	}

	callerDeadline := time.Now().Add(2 * defaultTimeout)
	callerContext, cancel := context.WithDeadline(t.Context(), callerDeadline)
	defer cancel()
	if _, err := client.Call(callerContext, &request); err != nil {
		t.Fatal(err)
	}
	if deadline, ok := invocationContext.Deadline(); !ok || !deadline.Equal(callerDeadline) {
		t.Errorf("explicit deadline = %v, want %v", deadline, callerDeadline)
	}
	if err := callerContext.Err(); err != nil {
		t.Errorf("call canceled the caller's context: %v", err)
	}
}

func TestUnaryClientClosesOnceAndRejectsLaterCalls(t *testing.T) {
	closeFailure := errors.New("close failed")
	var closes atomic.Int32
	client, err := rpc.NewUnaryClient(
		func(_ context.Context, request *string) (*string, error) { return request, nil },
		func() error {
			closes.Add(1)
			return closeFailure
		},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	var closing sync.WaitGroup
	for range 4 {
		closing.Go(func() {
			if err := client.Close(); !errors.Is(err, closeFailure) {
				t.Errorf("Close() = %v, want close failure", err)
			}
		})
	}
	closing.Wait()
	if got := closes.Load(); got != 1 {
		t.Errorf("connection closed %d times, want one", got)
	}
	if _, err := client.Call(t.Context(), new(string)); !errors.Is(err, rpc.ErrClientClosed) {
		t.Errorf("call after close = %v, want ErrClientClosed", err)
	}
}
