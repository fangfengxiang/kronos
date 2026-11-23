package assistant_test

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestPublicStreamValidatesApplicationCompletion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, health.NewServer())
	wire.RegisterAssistantServer(server, incompleteStreamService{})
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-serveResult; err != nil {
			t.Errorf("serve protocol test: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	address := listener.Addr().String()
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	upstream, err := wire.NewAssistantClient(connection).StreamRun(ctx, &wire.StreamRunRequest{Prompt: "missing completion"})
	if err != nil {
		t.Fatal(err)
	}
	if event, err := upstream.Recv(); err != nil || event.GetTextDelta() != "partial" {
		t.Fatalf("generated gRPC client received (%v, %v)", event, err)
	}
	if _, err := upstream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("generated gRPC client ended with %v, want EOF", err)
	}
	client, err := assistant.NewRPCClient(ctx, address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, prompt := range []string{"missing completion", "event after completion", "unknown event", "transport failure"} {
		t.Run(prompt, func(t *testing.T) {
			var output strings.Builder
			var completions int
			err := client.StreamRun(ctx, &assistant.StreamRunRequest{Prompt: prompt}, func(event *assistant.StreamRunResponse) error {
				if event.GetCompleted() != nil {
					completions++
				}
				output.WriteString(event.GetTextDelta())
				return nil
			})
			if prompt == "transport failure" {
				var publicError *rpc.Error
				if !errors.As(err, &publicError) || publicError.Code != rpc.Internal || strings.Contains(publicError.Message, "private transport error") {
					t.Fatalf("stream transport error = %v, want sanitized RPC error", err)
				}
			} else if !errors.Is(err, rpc.ErrInvalidStream) {
				t.Fatalf("stream protocol error = %v, want ErrInvalidStream", err)
			}
			if output.String() != "partial" || completions != 0 {
				t.Errorf("partial data = %q, completions = %d", output.String(), completions)
			}
		})
	}
}

type incompleteStreamService struct {
	wire.UnimplementedAssistantServer
}

func (incompleteStreamService) StreamRun(input *wire.StreamRunRequest, stream wire.Assistant_StreamRunServer) error {
	delta := &wire.StreamRunResponse{Event: &wire.StreamRunResponse_TextDelta{TextDelta: "partial"}}
	if err := stream.Send(delta); err != nil || input.GetPrompt() == "missing completion" {
		return err
	}
	if input.GetPrompt() == "unknown event" {
		return stream.Send(&wire.StreamRunResponse{})
	}
	completion := &wire.StreamRunResponse{Event: &wire.StreamRunResponse_Completed{Completed: &wire.RunCompleted{}}}
	if err := stream.Send(completion); err != nil {
		return err
	}
	if input.GetPrompt() == "transport failure" {
		return status.Error(codes.Internal, "private transport error")
	}
	return stream.Send(delta)
}
