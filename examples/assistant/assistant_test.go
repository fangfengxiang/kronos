package assistant_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/microv2"
	"google.golang.org/protobuf/proto"
)

type assistantService struct {
	requestStarted chan struct{}
	requestStopped chan error
}

const privateHandlerPanic = "private handler panic value"

func (service *assistantService) Run(ctx context.Context, input *assistant.RunRequest) (*assistant.RunResponse, error) {
	switch input.GetPrompt() {
	case "":
		return nil, &rpc.Error{
			Code:    rpc.InvalidArgument,
			Reason:  assistant.ErrorReasonInvalidPrompt.String(),
			Message: "请输入提示词",
		}
	case "fail":
		return nil, errors.New("private provider failure")
	case "panic":
		panic(privateHandlerPanic)
	case "request-id":
		return &assistant.RunResponse{Text: request.ID(ctx)}, nil
	case "wait":
		close(service.requestStarted)
		<-ctx.Done()
		service.requestStopped <- ctx.Err()
		return nil, ctx.Err()
	default:
		return &assistant.RunResponse{Text: input.GetPrompt()}, nil
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
			if transport.name != "gRPC" {
				err := client.StreamRun(ctx, &assistant.StreamRunRequest{}, func(*assistant.StreamRunResponse) error { return nil })
				if !errors.Is(err, rpc.ErrStreamingUnsupported) {
					t.Fatalf("%s stream error = %v, want ErrStreamingUnsupported", transport.name, err)
				}
			}

			response, err := client.Run(ctx, &assistant.RunRequest{Prompt: "你好"})
			if err != nil || response.GetText() != "你好" {
				t.Fatalf("Run() = (%v, %v)", response, err)
			}
			idRequest := &assistant.RunRequest{Prompt: "request-id"}
			generated, err := client.Run(ctx, idRequest)
			if err != nil || generated.GetText() == "" {
				t.Fatalf("run without a caller ID = (%v, %v)", generated, err)
			}
			const callerID = "assistant_request-01"
			withID, err := client.Run(request.WithID(ctx, callerID), idRequest)
			if err != nil || withID.GetText() == "" {
				t.Fatalf("run with a caller ID = (%v, %v)", withID, err)
			}
			if withID.GetText() != callerID || generated.GetText() == callerID {
				t.Fatalf("service received ID %q, want %q", withID.GetText(), callerID)
			}
			if transport.name == "HTTP" {
				assertMalformedJSONIsSanitized(t, ctx, listener.Addr().String())
				assertHTTPPanicReturnsFailure(t, ctx, listener.Addr().String())
			}
			if transport.name == "go-micro v2" {
				assertMicroV2PanicReturnsFailure(t, ctx, listener.Addr().String())
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
			_, err = client.Run(ctx, &assistant.RunRequest{Prompt: "panic"})
			if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != assistant.ErrorReasonRunFailed.String() || failure.Message != "assistant run failed" {
				t.Fatalf("recovered unary failure = %v", err)
			}
			response, err = client.Run(ctx, &assistant.RunRequest{Prompt: "after panic"})
			if err != nil || response.GetText() != "after panic" {
				t.Fatalf("unary call after recovered panic = %v, %v", response, err)
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
	response := postAssistant(t, ctx, address, `{"prompt": private_input}`)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "INVALID_REQUEST") || strings.Contains(string(body), "private_input") {
		t.Errorf("malformed JSON returned status %d and body %s", response.StatusCode, body)
	}
}

func assertHTTPPanicReturnsFailure(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	response := postAssistant(t, ctx, address, `{"prompt":"panic"}`)
	defer response.Body.Close()
	var failure struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusInternalServerError || failure.Reason != assistant.ErrorReasonRunFailed.String() || failure.Message != "assistant run failed" {
		t.Fatalf("HTTP panic response = status %d, body %+v", response.StatusCode, failure)
	}
}

func assertMicroV2PanicReturnsFailure(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	client, err := microv2.NewClient("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	payload, err := proto.Marshal(&assistant.RunRequest{Prompt: "panic"})
	if err != nil {
		t.Fatal(err)
	}
	const service = "kronos.assistant.v1.Assistant"
	_, err = client.Call(ctx, service, "Assistant.Run", payload)
	var failure *microv2.Error
	if !errors.As(err, &failure) || failure.Code != http.StatusInternalServerError || failure.Reason != assistant.ErrorReasonRunFailed.String() || failure.Message != "assistant run failed" {
		t.Fatalf("go-micro v2 panic response = %v", err)
	}
}

func postAssistant(t *testing.T, ctx context.Context, address, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/v1/assistant:run", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
