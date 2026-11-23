//go:build interop

package microv2_test

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

func TestGoMicroV2UpstreamInterop(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	server := assistant.NewMicroV2Server(listener, echoService{})
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Start(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			t.Errorf("stop Kronos server: %v", err)
		}
		select {
		case err := <-serveResult:
			if err != nil {
				t.Errorf("serve with Kronos: %v", err)
			}
		case <-ctx.Done():
			t.Error("Kronos server did not stop")
		}
	})

	t.Run("Kronos response limit", func(t *testing.T) {
		client, err := assistant.NewRPCClient(t.Context(), listener.Addr().String(), time.Second, rpc.WithProtocol(rpc.ProtocolMicroV2))
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		_, err = client.Run(t.Context(), &assistant.RunRequest{Prompt: "large response"})
		var failure *rpc.Error
		if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != assistant.ErrorReasonRunFailed.String() {
			t.Fatalf("public error for oversized response = %v", err)
		}
	})
	for _, version := range []string{"v2.0.0", "v2.9.1"} {
		t.Run(version, func(t *testing.T) {
			binary := buildUpstreamFixture(t, version)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, listener.Addr().String())
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("official client to Kronos: %v\n%s", err, output)
			}
			t.Logf("official client to Kronos:\n%s", output)
			address := startUpstreamFixture(t, ctx, binary)
			assertKronosClient(t, ctx, address)
		})
	}
}

type echoService struct{}

func (echoService) Run(_ context.Context, input *assistant.RunRequest) (*assistant.RunResponse, error) {
	if input.GetPrompt() == "large response" {
		return &assistant.RunResponse{Text: strings.Repeat("x", 1<<20)}, nil
	}
	if input.GetPrompt() == "" {
		return nil, &rpc.Error{Code: rpc.InvalidArgument, Reason: assistant.ErrorReasonInvalidPrompt.String(), Message: "prompt must not be blank"}
	}
	return &assistant.RunResponse{Text: input.GetPrompt()}, nil
}

func assertKronosClient(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	client, err := assistant.NewRPCClient(ctx, address, time.Second, rpc.WithProtocol(rpc.ProtocolMicroV2))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Kronos client: %v", err)
		}
	})
	response, err := client.Run(ctx, &assistant.RunRequest{Prompt: "你好"})
	if err != nil || response.GetText() != "你好" {
		t.Fatalf("Kronos client to official server: response=%v error=%v", response, err)
	}
	_, err = client.Run(ctx, &assistant.RunRequest{})
	var failure *rpc.Error
	if !errors.As(err, &failure) || failure.Code != rpc.InvalidArgument || failure.Reason != assistant.ErrorReasonInvalidPrompt.String() {
		t.Fatalf("Kronos client to official server: public error=%v", err)
	}
	t.Log("Kronos client to official server: Unicode response and public error verified")
}
