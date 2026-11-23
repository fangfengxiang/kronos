package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/infra/etcd"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) (runError error) {
	flags := flag.NewFlagSet("order-client", flag.ContinueOnError)
	endpoint := flags.String("etcd", "127.0.0.1:2379", "etcd host and port")
	protocol := flags.String("protocol", string(kronos.ProtocolGRPC), "grpc or micro-v2")
	service := flags.String("service", order.OrderServiceName, "service name for discovery")
	orderID := flags.String("id", "", "order ID to look up")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *orderID == "" {
		return errors.New("provide an order ID with -id")
	}
	registry, err := etcd.New(*endpoint)
	if err != nil {
		return err
	}
	defer func() { runError = errors.Join(runError, registry.Close()) }()
	client, err := kronos.NewClient(kronos.Registry(registry), kronos.Name(*service), kronos.Protocol(kronos.WireProtocol(*protocol)))
	if err != nil {
		return err
	}
	defer func() { runError = errors.Join(runError, client.Close()) }()
	response, err := order.NewOrderServiceClient(client).GetOrder(ctx, &order.GetOrderRequest{OrderId: *orderID})
	if err != nil {
		return err
	}
	fmt.Printf("%s %s\n", response.GetOrderId(), response.GetStatus())
	return nil
}
