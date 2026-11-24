package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	frameworklog "github.com/fangfengxiang/kronos/framework/log"
	"github.com/fangfengxiang/kronos/infra/etcd"
	"github.com/fangfengxiang/kronos/infra/oteltrace"
	"github.com/fangfengxiang/kronos/infra/prometheus"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func main() {
	if os.Getenv("KRONOS_ORDER_TRACE_ENDPOINT") != "" {
		slog.SetDefault(frameworklog.NewJSON(os.Stderr, nil))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runCommand(ctx); err != nil {
		log.Fatal(err)
	}
}

func runCommand(ctx context.Context) (runError error) {
	var options []kronos.Option
	if address := os.Getenv("KRONOS_ORDER_DIAGNOSTICS"); address != "" {
		metrics := prometheus.NewServiceRPCMetrics()
		options = append(options, kronos.Metrics(metrics), kronos.Diagnostics(address, metrics))
	}
	if endpoint := os.Getenv("KRONOS_ORDER_TRACE_ENDPOINT"); endpoint != "" {
		tracer, err := oteltrace.New(ctx, oteltrace.Config{ServiceName: orderServiceName(), Endpoint: endpoint})
		if err != nil {
			return err
		}
		defer func() {
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			runError = errors.Join(runError, tracer.Close(shutdown))
		}()
		options = append(options, kronos.Tracing(tracer))
	}
	return run(ctx, options...)
}

func run(ctx context.Context, optional ...kronos.Option) (runError error) {
	registry, err := etcd.New(setting("KRONOS_ETCD_ENDPOINT", "127.0.0.1:2379"))
	if err != nil {
		return err
	}
	defer func() { runError = errors.Join(runError, registry.Close()) }()
	options := []kronos.Option{
		kronos.Name(orderServiceName()),
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

func orderServiceName() string {
	return setting("KRONOS_ORDER_NAME", order.OrderServiceName)
}

func setting(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
