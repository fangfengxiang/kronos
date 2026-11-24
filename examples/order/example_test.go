package order_test

import (
	"fmt"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func ExampleBindOrderService() {
	application, err := kronos.New(kronos.Name(order.OrderServiceName), kronos.GRPC("127.0.0.1:9090"))
	if err != nil {
		panic(err)
	}
	if err := application.Register(order.BindOrderService(order.NewService())); err != nil {
		panic(err)
	}
	fmt.Println("Order registered")
	// Output: Order registered
}
