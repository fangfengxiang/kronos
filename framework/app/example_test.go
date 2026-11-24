package app_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/fangfengxiang/kronos/framework/app"
)

func ExampleApp_RunWithReadyHook() {
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	application, err := app.New(app.Config{ShutdownTimeout: time.Second}, app.Binding{
		Name:    "http",
		Address: "127.0.0.1:0",
		Build: func(listener net.Listener) (app.Server, error) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			return &exampleHTTPServer{listener: listener, server: &http.Server{Handler: mux}}, nil
		},
	})
	if err != nil {
		panic(err)
	}
	err = application.RunWithReadyHook(ctx, func(ctx context.Context, endpoints []app.Endpoint) (func(context.Context) error, error) {
		fmt.Println(application.Ready(), endpoints[0].Name)
		stop()
		return nil, nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(application.Ready())
	// Output:
	// true http
	// false
}

type exampleHTTPServer struct {
	listener net.Listener
	server   *http.Server
}

func (server *exampleHTTPServer) Start(context.Context) error {
	err := server.server.Serve(server.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (server *exampleHTTPServer) Stop(ctx context.Context) error {
	return server.server.Shutdown(ctx)
}
