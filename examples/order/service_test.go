package order

import (
	"context"
	"testing"

	wire "github.com/fangfengxiang/kronos/examples/order/internal/wire/kronos/order/v1"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func TestRegistrationChecksProtobufMethod(t *testing.T) {
	application, err := kronos.New(kronos.Name(OrderServiceName), kronos.GRPC("127.0.0.1:0"))
	if err != nil {
		t.Fatal(err)
	}
	descriptor := wire.File_kronos_order_v1_order_proto.Services().ByName("OrderService")
	badBinding := kronos.Bind(descriptor,
		kronos.Unary("GetOrder", &GetOrderResponse{}, &GetOrderResponse{},
			func(context.Context, *GetOrderResponse) (*GetOrderResponse, error) { return nil, nil }),
	)
	if err := application.Register(badBinding); err == nil {
		t.Fatal("registration accepted a method with the wrong request message")
	}
	if err := application.Register(BindOrderService(nil)); err == nil {
		t.Fatal("registration accepted a nil implementation")
	}
	service := BindOrderService(NewService())
	if err := application.Register(service); err != nil {
		t.Fatal(err)
	}
	if err := application.Register(service); err == nil {
		t.Fatal("registration accepted a duplicate service")
	}
}
