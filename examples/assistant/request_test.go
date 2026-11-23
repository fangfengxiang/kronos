package assistant_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"github.com/fangfengxiang/kronos/framework/request"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func TestGRPCStreamKeepsRequestID(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	server := assistant.NewGRPCServerWithStream(listener, &assistantService{}, testStream(func(ctx context.Context, _ *assistant.StreamRunRequest, emit func(string) error) error {
		for range 2 {
			if err := emit(request.ID(ctx)); err != nil {
				return err
			}
		}
		return nil
	}))
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

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	const callerID = "grpc-stream_01"
	ctx = metadata.AppendToOutgoingContext(ctx, request.IDHeader, callerID)
	stream, err := wire.NewAssistantClient(connection).StreamRun(ctx, &wire.StreamRunRequest{Prompt: "request ID"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		event, err := stream.Recv()
		if err != nil || event.GetTextDelta() != callerID {
			t.Fatalf("stream event = (%v, %v), want request ID %q", event, err, callerID)
		}
	}
	if terminal, err := stream.Recv(); err != nil || terminal.GetCompleted() == nil {
		t.Fatalf("stream terminal = (%v, %v), want completed", terminal, err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("stream end = %v, want EOF", err)
	}
}
