package assistant_test

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
)

func ExampleClient_StreamRun() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	service := testStream(func(_ context.Context, input *assistant.StreamRunRequest, emit func(string) error) error {
		return emit(input.GetPrompt())
	})
	server := assistant.NewGRPCServerWithStream(listener, &assistantService{}, service)
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.Start(context.Background()) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			panic(err)
		}
		if err := <-serverResult; err != nil {
			panic(err)
		}
	}()

	client, err := assistant.NewRPCClient(context.Background(), listener.Addr().String(), 5*time.Second)
	if err != nil {
		panic(err)
	}
	defer client.Close()
	input := &assistant.StreamRunRequest{Prompt: "你好"}
	err = client.StreamRun(context.Background(), input, func(event *assistant.StreamRunResponse) error {
		if event.GetCompleted() != nil {
			fmt.Println("completed")
		} else {
			fmt.Println(event.GetTextDelta())
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	// Output:
	// 你好
	// completed
}
