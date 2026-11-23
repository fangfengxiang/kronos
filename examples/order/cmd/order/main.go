package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/infra/etcd"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) (runError error) {
	registry, err := etcd.New(setting("KRONOS_ETCD_ENDPOINT", "127.0.0.1:2379"))
	if err != nil {
		return err
	}
	defer func() { runError = errors.Join(runError, registry.Close()) }()
	application, err := kronos.New(
		kronos.Name(setting("KRONOS_ORDER_NAME", order.OrderServiceName)),
		kronos.Registry(registry),
		kronos.GRPC(setting("KRONOS_ORDER_GRPC", "127.0.0.1:9090")),
		kronos.MicroRPC(setting("KRONOS_ORDER_MICRORPC", "127.0.0.1:9091")),
		kronos.AdvertiseHost(os.Getenv("KRONOS_ORDER_ADVERTISE_HOST")),
	)
	if err != nil {
		return err
	}
	if err := application.Register(order.BindOrderService(order.NewService())); err != nil {
		return err
	}
	return application.Run(ctx)
}

func setting(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
