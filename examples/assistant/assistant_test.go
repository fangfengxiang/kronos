package assistant_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

type assistantService struct {
	requestStarted chan struct{}
	requestStopped chan error
}

func (service *assistantService) Run(ctx context.Context, request *assistant.RunRequest) (*assistant.RunResponse, error) {
	switch request.GetPrompt() {
	case "":
		return nil, &rpc.Error{
			Code:    rpc.InvalidArgument,
			Reason:  assistant.ErrorReasonInvalidPrompt.String(),
			Message: "请输入提示词",
		}
	case "fail":
		return nil, errors.New("private provider failure")
	case "wait":
		close(service.requestStarted)
		<-ctx.Done()
		service.requestStopped <- ctx.Err()
		return nil, ctx.Err()
	default:
		return &assistant.RunResponse{Text: request.GetPrompt()}, nil
	}
}

func TestAssistantThroughKronosPublicAPI(t *testing.T) {
	transports := []struct {
		name           string
		endpointPrefix string
		newServer      func(net.Listener, assistant.Service) rpc.Server
		newClient      func(context.Context, string, time.Duration) (*assistant.Client, error)
	}{
		{"HTTP", "http://", assistant.NewHTTPServer, assistant.NewHTTPClient},
		{"gRPC", "", assistant.NewGRPCServer, func(ctx context.Context, endpoint string, timeout time.Duration) (*assistant.Client, error) {
			return assistant.NewRPCClient(ctx, endpoint, timeout)
		}},
		{"go-micro v2", "http://", assistant.NewMicroV2Server, func(ctx context.Context, endpoint string, timeout time.Duration) (*assistant.Client, error) {
			return assistant.NewRPCClient(ctx, endpoint, timeout, rpc.WithProtocol(rpc.ProtocolMicroV2))
		}},
	}
	for _, transport := range transports {
		t.Run(transport.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			service := &assistantService{requestStarted: make(chan struct{}), requestStopped: make(chan error, 1)}
			server := transport.newServer(listener, service)
			startResult := make(chan error, 1)
			go func() { startResult <- server.Start(context.Background()) }()
			t.Cleanup(func() {
				shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := server.Stop(shutdownContext); err != nil {
					t.Errorf("stop server: %v", err)
				}
				select {
				case err := <-startResult:
					if err != nil {
						t.Errorf("serve: %v", err)
					}
				case <-shutdownContext.Done():
					t.Error("server did not exit")
				}
			})

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			client, err := transport.newClient(ctx, transport.endpointPrefix+listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })

			response, err := client.Run(ctx, &assistant.RunRequest{Prompt: "你好"})
			if err != nil || response.GetText() != "你好" {
				t.Fatalf("Run() = (%v, %v)", response, err)
			}
			if transport.name == "HTTP" {
				assertMalformedJSONIsSanitized(t, ctx, listener.Addr().String())
			}
			_, err = client.Run(ctx, &assistant.RunRequest{})
			var failure *rpc.Error
			if !errors.As(err, &failure) || failure.Code != rpc.InvalidArgument || failure.Reason != assistant.ErrorReasonInvalidPrompt.String() || failure.Message != "请输入提示词" {
				t.Fatalf("invalid prompt error = %#v (%v)", failure, err)
			}
			_, err = client.Run(ctx, &assistant.RunRequest{Prompt: "fail"})
			if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != assistant.ErrorReasonRunFailed.String() || strings.Contains(err.Error(), "private provider failure") {
				t.Fatalf("unknown error was not translated: %#v (%v)", failure, err)
			}
			if errors.Unwrap(err) != nil {
				t.Errorf("public error carried a transport cause: %v", err)
			}

			requestContext, cancelRequest := context.WithCancel(ctx)
			defer cancelRequest()
			callResult := make(chan error, 1)
			go func() {
				_, err := client.Run(requestContext, &assistant.RunRequest{Prompt: "wait"})
				callResult <- err
			}()
			select {
			case <-service.requestStarted:
			case <-ctx.Done():
				t.Fatal("waiting call did not reach the service")
			}
			cancelRequest()
			select {
			case err := <-callResult:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("canceled call = %v, want context.Canceled", err)
				}
			case <-ctx.Done():
				t.Fatal("canceled call did not return")
			}
			select {
			case err := <-service.requestStopped:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("service context stopped with %v", err)
				}
			case <-ctx.Done():
				t.Error("cancellation did not reach the service")
			}
			if err := client.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Run(ctx, &assistant.RunRequest{Prompt: "after close"}); !errors.Is(err, rpc.ErrClientClosed) {
				t.Errorf("Run after Close = %v", err)
			}
		})
	}
}

func assertMalformedJSONIsSanitized(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/v1/assistant:run", strings.NewReader(`{"prompt": private_input}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "INVALID_REQUEST") || strings.Contains(string(body), "private_input") {
		t.Errorf("malformed JSON returned status %d and body %s", response.StatusCode, body)
	}
}
