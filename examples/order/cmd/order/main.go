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
	"github.com/fangfengxiang/kronos/infra/prometheus"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runCommand(ctx); err != nil {
		log.Fatal(err)
	}
}

func runCommand(ctx context.Context) error {
	address := os.Getenv("KRONOS_ORDER_DIAGNOSTICS")
	if address == "" {
		return run(ctx)
	}
	metrics, err := prometheus.NewRPCMetrics("/" + order.OrderServiceName + "/GetOrder")
	if err != nil {
		return err
	}
	return run(ctx, kronos.Metrics(metrics), kronos.Diagnostics(address, metrics))
}

func run(ctx context.Context, optional ...kronos.Option) (runError error) {
	registry, err := etcd.New(setting("KRONOS_ETCD_ENDPOINT", "127.0.0.1:2379"))
	if err != nil {
		return err
	}
	defer func() { runError = errors.Join(runError, registry.Close()) }()
	options := []kronos.Option{
		kronos.Name(setting("KRONOS_ORDER_NAME", order.OrderServiceName)),
		kronos.Registry(registry),
		kronos.GRPC(setting("KRONOS_ORDER_GRPC", "127.0.0.1:9090")),
		kronos.MicroRPC(setting("KRONOS_ORDER_MICRORPC", "127.0.0.1:9091")),
		kronos.AdvertiseHost(os.Getenv("KRONOS_ORDER_ADVERTISE_HOST")),
	}
	application, err := kronos.New(append(options, optional...)...)
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
