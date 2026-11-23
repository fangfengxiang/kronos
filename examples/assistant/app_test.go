package assistant_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/app"
)

type waitingService struct {
	started chan struct{}
	finish  chan struct{}
	result  chan error
}

func (service waitingService) Run(ctx context.Context, request *assistant.RunRequest) (*assistant.RunResponse, error) {
	service.started <- struct{}{}
	select {
	case <-service.finish:
		service.result <- nil
		return &assistant.RunResponse{Text: request.GetPrompt()}, nil
	case <-ctx.Done():
		service.result <- ctx.Err()
		return nil, ctx.Err()
	}
}

func TestApplicationDrainsRealAssistantCalls(t *testing.T) {
	for _, test := range []struct {
		name            string
		shutdownTimeout time.Duration
		finish          bool
	}{
		{"finishes both calls", 2 * time.Second, true},
		{"cancels both calls", 150 * time.Millisecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			checkContext, cancelCheck := context.WithTimeout(t.Context(), 6*time.Second)
			defer cancelCheck()
			runContext, stop := context.WithCancel(checkContext)
			defer stop()
			httpAddress := make(chan string, 1)
			grpcAddress := make(chan string, 1)
			service := waitingService{make(chan struct{}, 2), make(chan struct{}), make(chan error, 2)}
			application, err := app.New(app.Config{ShutdownTimeout: test.shutdownTimeout},
				app.Binding{Name: "HTTP", Address: "127.0.0.1:0", Build: func(listener net.Listener) (app.Server, error) {
					httpAddress <- "http://" + listener.Addr().String()
					return assistant.NewHTTPServer(listener, service), nil
				}},
				app.Binding{Name: "gRPC", Address: "127.0.0.1:0", Build: func(listener net.Listener) (app.Server, error) {
					grpcAddress <- listener.Addr().String()
					return assistant.NewGRPCServer(listener, service), nil
				}},
			)
			if err != nil {
				t.Fatal(err)
			}
			exitResult := make(chan error, 1)
			go func() { exitResult <- application.Run(runContext) }()
			httpClient, err := assistant.NewHTTPClient(checkContext, await(t, checkContext, httpAddress), 0)
			if err != nil {
				t.Fatal(err)
			}
			defer httpClient.Close()
			grpcClient, err := assistant.NewGRPCClient(checkContext, await(t, checkContext, grpcAddress), 0)
			if err != nil {
				t.Fatal(err)
			}
			defer grpcClient.Close()
			callResults := make(chan error, 2)
			for _, client := range []*assistant.Client{httpClient, grpcClient} {
				go func() {
					_, err := client.Run(checkContext, &assistant.RunRequest{Prompt: "continue"})
					callResults <- err
				}()
			}
			for range 2 {
				await(t, checkContext, service.started)
			}
			readinessChecks := time.NewTicker(time.Millisecond)
			defer readinessChecks.Stop()
			for !application.Ready() {
				await(t, checkContext, readinessChecks.C)
			}
			stop()
			if test.finish {
				for application.Ready() {
					await(t, checkContext, readinessChecks.C)
				}
				close(service.finish)
			}
			for range 2 {
				requestError := await(t, checkContext, service.result)
				callError := await(t, checkContext, callResults)
				if test.finish && (requestError != nil || callError != nil) {
					t.Fatalf("request failed inside shutdown budget: service=%v caller=%v", requestError, callError)
				}
				if !test.finish && (!errors.Is(requestError, context.Canceled) || callError == nil) {
					t.Fatalf("request survived shutdown deadline: service=%v caller=%v", requestError, callError)
				}
			}
			exitError := await(t, checkContext, exitResult)
			if test.finish && exitError != nil || !test.finish && !errors.Is(exitError, context.DeadlineExceeded) {
				t.Errorf("shutdown error = %v", exitError)
			}
		})
	}
}

func await[Value any](t *testing.T, ctx context.Context, values <-chan Value) Value {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-ctx.Done():
		t.Fatalf("awaiting application event: %v", ctx.Err())
		var zero Value
		return zero
	}
}
