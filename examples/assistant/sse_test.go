package assistant_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"iter"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/infra/blades"
)

type testStream func(context.Context, *assistant.StreamRunRequest, func(string) error) error

func (service testStream) StreamRun(ctx context.Context, request *assistant.StreamRunRequest, emit func(string) error) error {
	return service(ctx, request, emit)
}

func TestSSEFailureAndDisconnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	providerFailure := errors.New("hidden provider error")
	workStopped := make(chan error, 1)
	const callerID = "sse-client.01"
	const privateModelPanic = "private model panic"
	runner, err := blades.New("assistant", func(ctx context.Context, prompt string) iter.Seq2[string, error] {
		return func(yield func(string, error) bool) {
			if id := request.ID(ctx); id != callerID {
				t.Errorf("SSE model context ID = %q, want %q", id, callerID)
			}
			switch prompt {
			case "fail early":
				yield("", providerFailure)
				return
			case "model panic early":
				panic(privateModelPanic)
			case "after panic":
				yield("已恢复", nil)
				return
			}
			if !yield("首段", nil) {
				return
			}
			switch prompt {
			case "fail late":
				yield("", providerFailure)
				return
			case "model panic late":
				panic(privateModelPanic)
			}
			<-ctx.Done()
			workStopped <- ctx.Err()
			yield("", ctx.Err())
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	server := assistant.NewSSEServer(listener, testStream(func(ctx context.Context, request *assistant.StreamRunRequest, emit func(string) error) error {
		switch request.GetPrompt() {
		case "panic early":
			panic(privateHandlerPanic)
		case "panic late":
			if err := emit("首段"); err != nil {
				return err
			}
			panic(privateHandlerPanic)
		}
		return runner.Stream(ctx, request.GetPrompt(), emit)
	}))
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Start(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			t.Error(err)
		}
		if err := await(t, ctx, serveResult); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	endpoint := "http://" + listener.Addr().String() + "/v1/assistant:streamRun"
	post := func(body string) *http.Response {
		t.Helper()
		httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest.Header.Set("Accept", "text/event-stream")
		httpRequest.Header.Set(request.IDHeader, callerID)
		response, err := http.DefaultClient.Do(httpRequest)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	for _, test := range []struct {
		body        string
		status      int
		streamError bool
	}{
		{`{"prompt":"fail early"}`, http.StatusInternalServerError, false},
		{`{"prompt":"fail late"}`, http.StatusOK, true},
		{`{"prompt":"panic early"}`, http.StatusInternalServerError, false},
		{`{"prompt":"panic late"}`, http.StatusOK, true},
		{`{"prompt":"model panic early"}`, http.StatusInternalServerError, false},
		{`{"prompt":"model panic late"}`, http.StatusOK, true},
	} {
		response := post(test.body)
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		bodyText := string(body)
		if err != nil || response.StatusCode != test.status || !strings.Contains(bodyText, "ERROR_REASON_RUN_FAILED") ||
			strings.Contains(bodyText, providerFailure.Error()) || strings.Contains(bodyText, privateHandlerPanic) ||
			strings.Contains(bodyText, privateModelPanic) || strings.Contains(bodyText, `"completed"`) ||
			strings.Contains(bodyText, "event: error") != test.streamError || strings.Contains(bodyText, "首段") != test.streamError {
			t.Fatalf("SSE failure = %d %s (%v)", response.StatusCode, body, err)
		}
	}
	response := post(`{"prompt":"after panic"}`)
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "已恢复") || strings.Count(string(body), `"completed"`) != 1 {
		t.Fatalf("SSE after recovered panic = %d %s (%v)", response.StatusCode, body, err)
	}
	response = post(`{"prompt":"wait"}`)
	if firstLine, err := bufio.NewReader(response.Body).ReadString('\n'); err != nil || firstLine != "event: message\n" {
		response.Body.Close()
		t.Fatalf("first event = %q, %v", firstLine, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := await(t, ctx, workStopped); !errors.Is(err, context.Canceled) {
		t.Errorf("stream context ended with %v", err)
	}
}
