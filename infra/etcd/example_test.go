package etcd_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/fangfengxiang/kronos/infra/etcd"
)

func ExampleRegistry_Lookup() {
	registry, err := etcd.New("127.0.0.1:2379")
	if err != nil {
		panic(err)
	}
	defer registry.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = registry.Lookup(ctx, "orders", "grpc")
	fmt.Println(errors.Is(err, context.Canceled))
	// Output: true
}
