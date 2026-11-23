package app_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/framework/app"
)

type testServer struct {
	start func(context.Context) error
	stop  func(context.Context) error
}

func (server testServer) Start(ctx context.Context) error { return server.start(ctx) }
func (server testServer) Stop(ctx context.Context) error  { return server.stop(ctx) }

func TestRunReleasesFirstEndpointWhenSecondPortIsOccupied(t *testing.T) {
	first := listen(t, "127.0.0.1:0")
	firstAddress := first.Addr().String()
	occupied := listen(t, "127.0.0.1:0")
	defer occupied.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	build := func(net.Listener) (app.Server, error) {
		t.Fatal("building began before all listeners were acquired")
		return nil, nil
	}
	application, err := app.New(app.Config{ShutdownTimeout: time.Second},
		app.Binding{Name: "first", Address: firstAddress, Build: build},
		app.Binding{Name: "occupied", Address: occupied.Addr().String(), Build: build},
	)
	if err != nil {
		t.Fatal(err)
	}
	var listenError *net.OpError
	if err := application.Run(t.Context()); !errors.As(err, &listenError) {
		t.Fatalf("Run() = %v, want listener error", err)
	}
	if application.Ready() {
		t.Error("application advertised readiness after bind failure")
	}
	if err := application.Run(t.Context()); !errors.Is(err, app.ErrAlreadyRun) {
		t.Errorf("second Run() = %v, want ErrAlreadyRun", err)
	}
	_ = listen(t, firstAddress).Close()
}

func TestRunPreservesStartAndStopErrors(t *testing.T) {
	serveFailure := errors.New("serve failed")
	stopFailure := errors.New("stop failed")
	var address string
	application, err := app.New(app.Config{ShutdownTimeout: time.Second}, app.Binding{
		Name: "test", Address: "127.0.0.1:0",
		Build: func(listener net.Listener) (app.Server, error) {
			address = listener.Addr().String()
			return testServer{
				start: func(context.Context) error { return serveFailure },
				stop:  func(context.Context) error { return stopFailure },
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.Run(t.Context()); !errors.Is(err, serveFailure) || !errors.Is(err, stopFailure) {
		t.Fatalf("Run() = %v, want both serve and cleanup failures", err)
	}
	if application.Ready() {
		t.Error("application advertised readiness after serve failure")
	}
	_ = listen(t, address).Close()
}

func TestRunReleasesListenersAfterBuildFailure(t *testing.T) {
	buildFailure := errors.New("registration failed")
	rollbackFailure := errors.New("rollback failed")
	var addresses []string
	application, err := app.New(app.Config{ShutdownTimeout: time.Second},
		app.Binding{Name: "first", Address: "127.0.0.1:0", Build: func(listener net.Listener) (app.Server, error) {
			addresses = append(addresses, listener.Addr().String())
			return testServer{
				start: func(context.Context) error {
					t.Error("started a server after a build failure")
					return nil
				},
				stop: func(context.Context) error { return rollbackFailure },
			}, nil
		}},
		app.Binding{Name: "second", Address: "127.0.0.1:0", Build: func(listener net.Listener) (app.Server, error) {
			addresses = append(addresses, listener.Addr().String())
			return nil, buildFailure
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.Run(t.Context()); !errors.Is(err, buildFailure) || !errors.Is(err, rollbackFailure) {
		t.Fatalf("Run() = %v, want build and rollback failures", err)
	}
	for _, address := range addresses {
		_ = listen(t, address).Close()
	}
}

func TestReadinessAndShutdownOfAdmittedWork(t *testing.T) {
	for _, test := range []struct {
		name            string
		shutdownTimeout time.Duration
		finishWork      bool
	}{
		{"finishes within budget", time.Second, true},
		{"cancels after budget", 80 * time.Millisecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			runContext, stopApplication := context.WithCancel(t.Context())
			defer stopApplication()
			address := make(chan string, 1)
			allowAccept := make(chan struct{})
			startWork := make(chan context.Context, 1)
			finishWork := make(chan struct{})
			workResult := make(chan error, 1)
			workDone := make(chan struct{})
			stopEntered := make(chan context.Context, 1)

			application, err := app.New(app.Config{ShutdownTimeout: test.shutdownTimeout}, app.Binding{
				Name: "test", Address: "127.0.0.1:0",
				Build: func(listener net.Listener) (app.Server, error) {
					address <- listener.Addr().String()
					return testServer{
						start: func(ctx context.Context) error {
							defer close(workDone)
							select {
							case <-allowAccept:
							case <-ctx.Done():
								return ctx.Err()
							}
							connection, err := listener.Accept()
							if err != nil {
								return err
							}
							defer connection.Close()
							startWork <- ctx
							select {
							case <-finishWork:
								workResult <- nil
							case <-ctx.Done():
								workResult <- ctx.Err()
							}
							return nil
						},
						stop: func(ctx context.Context) error {
							stopEntered <- ctx
							select {
							case <-workDone:
								return nil
							case <-ctx.Done():
								return ctx.Err()
							}
						},
					}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			runResult := make(chan error, 1)
			go func() { runResult <- application.Run(runContext) }()
			probes := application.Probes()
			serverAddress := receive(t, address)
			assertProbe(t, probes, "/livez", http.StatusOK)
			assertProbe(t, probes, "/readyz", http.StatusServiceUnavailable)
			close(allowAccept)
			connection, err := net.DialTimeout("tcp", serverAddress, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			workContext := receive(t, startWork)
			waitUntilReady(t, application)
			assertProbe(t, probes, "/readyz", http.StatusOK)
			stopApplication()
			shutdownContext := receive(t, stopEntered)
			assertProbe(t, probes, "/readyz", http.StatusServiceUnavailable)
			if _, hasDeadline := shutdownContext.Deadline(); !hasDeadline {
				t.Error("stop context has no deadline")
			}
			if test.finishWork {
				if err := workContext.Err(); err != nil {
					t.Fatalf("request canceled before budget expired: %v", err)
				}
				close(finishWork)
				if err := receive(t, workResult); err != nil {
					t.Errorf("in-flight request failed: %v", err)
				}
				if err := receive(t, runResult); err != nil {
					t.Errorf("Run() = %v, want clean shutdown", err)
				}
			} else {
				if err := receive(t, workResult); !errors.Is(err, context.Canceled) {
					t.Errorf("in-flight request = %v, want canceled", err)
				}
				if err := receive(t, runResult); !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("Run() = %v, want shutdown timeout", err)
				}
			}
			_ = listen(t, serverAddress).Close()
		})
	}
}

func listen(t *testing.T, address string) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	return listener
}

func receive[Value any](t *testing.T, values <-chan Value) Value {
	t.Helper()
	deadline, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	select {
	case value := <-values:
		return value
	case <-deadline.Done():
		t.Fatal("timed out waiting for application lifecycle")
		var zero Value
		return zero
	}
}

func waitUntilReady(t *testing.T, application *app.App) {
	t.Helper()
	deadline, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !application.Ready() {
		select {
		case <-ticker.C:
		case <-deadline.Done():
			t.Fatal("application did not publish readiness")
		}
	}
}

func assertProbe(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != want {
		t.Errorf("%s status = %d, want %d", path, response.Code, want)
	}
}
