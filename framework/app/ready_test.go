package app_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/framework/app"
)

func TestReadyHookPublishesAfterAcceptAndWithdrawsBeforeStop(t *testing.T) {
	publishFailure := errors.New("publish failed")
	withdrawFailure := errors.New("withdraw failed")
	for _, scenario := range []struct {
		name          string
		publishError  error
		withdrawError error
	}{
		{name: "normal stop"},
		{name: "partial startup", publishError: publishFailure, withdrawError: withdrawFailure},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runContext, stop := context.WithCancel(t.Context())
			defer stop()
			publishedAddress := make(chan string, 1)
			var withdrawDeadline time.Time
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})}
			application, err := app.New(app.Config{ShutdownTimeout: 2 * time.Second}, app.Binding{
				Name: "public", Address: "127.0.0.1:0",
				Build: func(listener net.Listener) (app.Server, error) {
					return testServer{
						start: func(context.Context) error {
							if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
								return err
							}
							return nil
						},
						stop: func(ctx context.Context) error {
							deadline, ok := ctx.Deadline()
							if !ok || withdrawDeadline.IsZero() || !deadline.Equal(withdrawDeadline) {
								t.Errorf("server shutdown deadline %v differs from withdrawal deadline %v", deadline, withdrawDeadline)
							}
							return server.Shutdown(ctx)
						},
					}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			runResult := make(chan error, 1)
			go func() {
				runResult <- application.RunWithReadyHook(runContext, func(ctx context.Context, endpoints []app.Endpoint) (func(context.Context) error, error) {
					if !application.Ready() || len(endpoints) != 1 || endpoints[0].Name != "public" {
						return nil, errors.New("hook ran before the listener was ready")
					}
					address := endpoints[0].Address
					client := &http.Client{Transport: &http.Transport{}, Timeout: time.Second}
					defer client.CloseIdleConnections()
					request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address, nil)
					if err != nil {
						return nil, err
					}
					response, err := client.Do(request)
					if err != nil {
						return nil, err
					}
					response.Body.Close()
					if response.StatusCode != http.StatusNoContent {
						return nil, errors.New("listener did not serve a request during the hook")
					}
					publishedAddress <- address
					return func(ctx context.Context) error {
						if application.Ready() || ctx.Err() != nil {
							t.Error("withdrawal must run after readiness is removed and with an active shutdown budget")
						}
						withdrawDeadline, _ = ctx.Deadline()
						return scenario.withdrawError
					}, scenario.publishError
				})
			}()
			address := receive(t, publishedAddress)
			if scenario.publishError == nil {
				stop()
			}
			if err := receive(t, runResult); !errors.Is(err, scenario.publishError) || !errors.Is(err, scenario.withdrawError) {
				t.Fatalf("RunWithReadyHook = %v, want publication %v and withdrawal %v", err, scenario.publishError, scenario.withdrawError)
			}
			if application.Ready() {
				t.Error("application is still ready after shutdown")
			}
			_ = listen(t, address).Close()
		})
	}
}
