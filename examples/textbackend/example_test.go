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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	server, err := textbackend.NewGRPCServer(listener, textService{})
	if err != nil {
		panic(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Start(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := textbackend.NewGRPCClient(ctx, listener.Addr().String(), time.Second)
	if err != nil {
		panic(err)
	}
	response, err := client.Generate(ctx, &textbackend.GenerateRequest{Prompt: "你好"})
	if err != nil {
		panic(err)
	}
	if err := client.Close(); err != nil {
		panic(err)
	}
	if err := server.Stop(ctx); err != nil {
		panic(err)
	}
	if err := <-served; err != nil {
		panic(err)
	}
	fmt.Println(response.GetText())
	// Output: backend: 你好
}
