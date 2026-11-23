package textbackend_test

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
)

type textService struct{}

func (textService) Generate(_ context.Context, input *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error) {
	return &textbackend.GenerateResponse{Text: "backend: " + input.GetPrompt()}, nil
}

func ExampleNewGRPCClient() {
	ctx, endpoint, stop := startExampleBackend()
	defer stop()
	client, err := textbackend.NewGRPCClient(ctx, endpoint, time.Second)
	if err != nil {
		panic(err)
	}
	defer client.Close()
	response, err := client.Generate(ctx, &textbackend.GenerateRequest{Prompt: "你好"})
	if err != nil {
		panic(err)
	}
	fmt.Println(response.GetText())
	// Output: backend: 你好
}

func ExampleNewDiscoveredGRPCClient() {
	ctx, endpoint, stop := startExampleBackend()
	defer stop()
	discovery := exampleDiscovery{endpoint: endpoint}
	client, err := textbackend.NewDiscoveredGRPCClient(ctx, textbackend.DefaultServiceName, time.Second, discovery)
	if err != nil {
		panic(err)
	}
	defer client.Close()
	response, err := client.Generate(ctx, &textbackend.GenerateRequest{Prompt: "你好"})
	if err != nil {
		panic(err)
	}
	fmt.Println(response.GetText())
	// Output: backend: 你好
}

type exampleDiscovery struct {
	endpoint string
}

func (discovery exampleDiscovery) Watch(ctx context.Context, _ string, observe func([]string)) error {
	observe([]string{discovery.endpoint})
	<-ctx.Done()
	return ctx.Err()
}

func startExampleBackend() (context.Context, string, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	server, err := textbackend.NewGRPCServer(listener, textService{})
	if err != nil {
		listener.Close()
		panic(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Start(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	return ctx, listener.Addr().String(), func() {
		defer cancel()
		defer listener.Close()
		if err := server.Stop(ctx); err != nil {
			panic(err)
		}
		if err := <-served; err != nil {
			panic(err)
		}
	}
}
