package assistant_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
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
	server := assistant.NewSSEServer(listener, testStream(func(ctx context.Context, request *assistant.StreamRunRequest, emit func(string) error) error {
		if request.GetPrompt() == "fail early" {
			return providerFailure
		}
		if err := emit("首段"); err != nil {
			return err
		}
		if request.GetPrompt() == "fail late" {
			return providerFailure
		}
		<-ctx.Done()
		workStopped <- ctx.Err()
		return ctx.Err()
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
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "text/event-stream")
		response, err := http.DefaultClient.Do(request)
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
	} {
		response := post(test.body)
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		bodyText := string(body)
		if err != nil || response.StatusCode != test.status || !strings.Contains(bodyText, "ERROR_REASON_RUN_FAILED") ||
			strings.Contains(bodyText, providerFailure.Error()) || strings.Contains(bodyText, `"completed"`) ||
			strings.Contains(bodyText, "event: error") != test.streamError {
			t.Fatalf("SSE failure = %d %s (%v)", response.StatusCode, body, err)
		}
	}
	response := post(`{"prompt":"wait"}`)
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
