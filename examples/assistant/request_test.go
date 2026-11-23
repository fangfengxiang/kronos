package assistant_test

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

func TestGRPCStreamKeepsRequestID(t *testing.T) {
	address := startGRPCStreamServer(t, func(ctx context.Context, _ *assistant.StreamRunRequest, emit func(string) error) error {
		for range 2 {
			if err := emit(request.ID(ctx)); err != nil {
				return err
			}
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := assistant.NewRPCClient(ctx, address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	const callerID = "grpc-stream_01"
	var deltas []string
	completions := 0
	err = client.StreamRun(request.WithID(ctx, callerID), &assistant.StreamRunRequest{Prompt: "request ID"}, func(event *assistant.StreamRunResponse) error {
		if event.GetCompleted() != nil {
			completions++
		} else {
			deltas = append(deltas, event.GetTextDelta())
		}
		return nil
	})
	if err != nil || !slices.Equal(deltas, []string{callerID, callerID}) || completions != 1 {
		t.Fatalf("stream result has deltas %q, completions %d and error %v", deltas, completions, err)
	}
}

func TestGRPCStreamCancelsWhenReceiverStops(t *testing.T) {
	serverCanceled := make(chan error, 1)
	address := startGRPCStreamServer(t, func(ctx context.Context, _ *assistant.StreamRunRequest, emit func(string) error) error {
		if err := emit("first"); err != nil {
			return err
		}
		<-ctx.Done()
		serverCanceled <- ctx.Err()
		return ctx.Err()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := assistant.NewRPCClient(ctx, address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	input := &assistant.StreamRunRequest{Prompt: "cancel after first event"}
	receiverFailure := errors.New("receiver stopped")
	err = client.StreamRun(ctx, input, func(event *assistant.StreamRunResponse) error {
		if event.GetTextDelta() != "first" {
			t.Errorf("first event = %v", event)
		}
		return receiverFailure
	})
	if err != receiverFailure {
		t.Fatalf("stream returned %v, want original receiver failure", err)
	}
	if err := await(t, ctx, serverCanceled); !errors.Is(err, context.Canceled) {
		t.Errorf("server context error = %v, want canceled", err)
	}
	expired, cancelExpired := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancelExpired()
	err = client.StreamRun(expired, input, func(*assistant.StreamRunResponse) error {
		t.Error("expired stream received an event")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expired stream returned %v, want deadline exceeded", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.StreamRun(ctx, input, func(*assistant.StreamRunResponse) error { return nil }); !errors.Is(err, rpc.ErrClientClosed) {
		t.Fatalf("stream after close = %v, want ErrClientClosed", err)
	}
}

func startGRPCStreamServer(t *testing.T, stream testStream) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	server := assistant.NewGRPCServerWithStream(listener, &assistantService{}, stream)
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Start(context.Background()) }()
	t.Cleanup(func() {
		shutdownContext, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := server.Stop(shutdownContext); err != nil {
			t.Error(err)
		}
		if err := await(t, shutdownContext, serveResult); err != nil {
			t.Error(err)
		}
	})
	return listener.Addr().String()
}
