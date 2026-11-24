package microv2_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"time"

	"github.com/fangfengxiang/kronos/infra/microv2"
)

func ExampleClient_Call() {
	handler := microv2.NewHandler("echo", map[string]microv2.Unary{
		"Echo.Repeat": func(_ context.Context, payload []byte) ([]byte, error) {
			return payload, nil
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := microv2.NewClient(server.URL)
	if err != nil {
		panic(err)
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// A serialized Protobuf message with string field 1 set to "ok".
	request := []byte{0x0a, 0x02, 'o', 'k'}
	response, err := client.Call(ctx, "echo", "Echo.Repeat", request)
	if err != nil {
		panic(err)
	}
	fmt.Println(bytes.Equal(response, request))
	// Output: true
}
