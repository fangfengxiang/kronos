// This standalone consumer is built against one upstream go-micro version at a time.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/micro/go-micro/v2/client"
	microerrors "github.com/micro/go-micro/v2/errors"
)

type Request struct {
	Prompt string `protobuf:"bytes,1,opt,name=prompt,proto3"`
}

func (message *Request) Reset()         { *message = Request{} }
func (message *Request) String() string { return proto.CompactTextString(message) }
func (*Request) ProtoMessage()          {}

type Response struct {
	Text string `protobuf:"bytes,1,opt,name=text,proto3"`
}

func (message *Response) Reset()         { *message = Response{} }
func (message *Response) String() string { return proto.CompactTextString(message) }
func (*Response) ProtoMessage()          {}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "serve" {
		if err := serve(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) != 2 {
		log.Fatal("usage: microv2-client address")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rpcClient := client.NewClient(client.Retries(0))
	for _, contentType := range []string{"application/protobuf", "application/proto-rpc"} {
		newRequest := func(prompt string) client.Request {
			return rpcClient.NewRequest("kronos.assistant.v1.Assistant", "Assistant.Run", &Request{Prompt: prompt}, client.WithContentType(contentType))
		}
		var result Response
		if err := rpcClient.Call(ctx, newRequest("你好"), &result, client.WithAddress(os.Args[1])); err != nil || result.Text != "你好" {
			log.Fatalf("%s success = (%s, %v)", contentType, result.Text, err)
		}
		if err := rpcClient.Call(ctx, newRequest(""), &result, client.WithAddress(os.Args[1])); err == nil {
			log.Fatalf("%s accepted empty prompt", contentType)
		} else if failure := microerrors.Parse(err.Error()); failure.Code != 400 || failure.Id != "ERROR_REASON_INVALID_PROMPT" {
			log.Fatalf("%s returned the wrong public error: %v", contentType, failure)
		}
		fmt.Println(contentType, "success and public error verified")
		err := rpcClient.Call(ctx, newRequest("large response"), &result, client.WithAddress(os.Args[1]))
		if contentType == "application/proto-rpc" {
			if err == nil {
				log.Fatal("framed client accepted an oversized response")
			}
			if failure := microerrors.Parse(err.Error()); failure.Code != 500 || failure.Id != "RESPONSE_TOO_LARGE" {
				log.Fatalf("framed client returned the wrong size error: %v", failure)
			}
		} else if err != nil || len(result.Text) != 1<<20 {
			log.Fatalf("raw Protobuf client received %d bytes, error %v", len(result.Text), err)
		}
		fmt.Println(contentType, "large response behavior verified")
	}
}
