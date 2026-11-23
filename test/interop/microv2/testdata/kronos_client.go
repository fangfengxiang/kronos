// This standalone consumer exercises the Kronos connection to the upstream server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: kronos-client address")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := assistant.NewRPCClient(ctx, os.Args[1], time.Second, rpc.WithProtocol(rpc.ProtocolMicroV2))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	result, err := client.Run(ctx, &assistant.RunRequest{Prompt: "你好"})
	if err != nil || result.GetText() != "你好" {
		log.Fatalf("Kronos client to upstream success = %v, %v", result, err)
	}
	_, err = client.Run(ctx, &assistant.RunRequest{})
	var failure *rpc.Error
	if !errors.As(err, &failure) || failure.Code != rpc.InvalidArgument || failure.Reason != assistant.ErrorReasonInvalidPrompt.String() {
		log.Fatalf("Kronos client to upstream public error = %v", err)
	}
	fmt.Println("Kronos client to upstream server: application/proto-rpc success and public error verified")
}
