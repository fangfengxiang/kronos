package request_test

import (
	"context"
	"fmt"

	"github.com/fangfengxiang/kronos/framework/request"
)

func ExampleWithID() {
	ctx := request.WithID(context.Background(), "gateway_42")
	fmt.Println(request.ID(ctx))
	// Output: gateway_42
}
