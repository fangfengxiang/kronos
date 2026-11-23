package rpc_test

import (
	"fmt"

	"github.com/fangfengxiang/kronos/framework/rpc"
)

func ExampleWithProtocol() {
	options := []rpc.ClientOption{rpc.WithProtocol(rpc.ProtocolMicroV2)}
	protocol, err := rpc.ResolveProtocol(options...)
	if err != nil {
		panic(err)
	}
	fmt.Println(protocol)
	// Output: micro-v2
}
