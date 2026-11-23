package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/golang/protobuf/proto"
	memorybroker "github.com/micro/go-micro/v2/broker/memory"
	"github.com/micro/go-micro/v2/client"
	microerrors "github.com/micro/go-micro/v2/errors"
	"github.com/micro/go-micro/v2/registry"
	microetcd "github.com/micro/go-micro/v2/registry/etcd"
	"github.com/micro/go-micro/v2/server"
)

func main() {
	if len(os.Args) < 4 {
		log.Fatal("usage: etcdregistry address service inspect|order|serve")
	}
	discovery := microetcd.NewRegistry(registry.Addrs(os.Args[1]), registry.Timeout(3*time.Second))
	if os.Args[3] == "serve" && len(os.Args) == 5 {
		if err := serveOrder(discovery, os.Args[2], os.Args[4]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if os.Args[3] == "order" {
		callOrder(discovery, os.Args[2])
		return
	}
	if os.Args[3] != "inspect" {
		log.Fatal("unknown etcdregistry command")
	}
	services, err := discovery.GetService(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	for _, service := range services {
		for _, node := range service.Nodes {
			fmt.Println(service.Name, service.Version, node.Id, node.Address)
		}
	}
}

type OrderRequest struct {
	OrderID string `protobuf:"bytes,1,opt,name=order_id,json=orderId,proto3"`
}

func (message *OrderRequest) Reset()         { *message = OrderRequest{} }
func (message *OrderRequest) String() string { return proto.CompactTextString(message) }
func (*OrderRequest) ProtoMessage()          {}

type OrderResponse struct {
	OrderID string `protobuf:"bytes,1,opt,name=order_id,json=orderId,proto3"`
	Status  string `protobuf:"bytes,2,opt,name=status,proto3"`
}

func (message *OrderResponse) Reset()         { *message = OrderResponse{} }
func (message *OrderResponse) String() string { return proto.CompactTextString(message) }
func (*OrderResponse) ProtoMessage()          {}

func callOrder(discovery registry.Registry, service string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rpcClient := client.NewClient(client.Registry(discovery), client.Retries(0))
	for _, encoding := range []string{"application/protobuf", "application/proto-rpc"} {
		request := func(id string) client.Request {
			return rpcClient.NewRequest(service, "OrderService.GetOrder", &OrderRequest{OrderID: id}, client.WithContentType(encoding))
		}
		var response OrderResponse
		if err := rpcClient.Call(ctx, request("订单-01"), &response); err != nil || response.OrderID != "订单-01" || response.Status != "confirmed" {
			log.Fatalf("%s discovered Order: %s, %v", encoding, &response, err)
		}
		if err := rpcClient.Call(ctx, request(""), &response); err == nil {
			log.Fatalf("%s accepted a blank order ID", encoding)
		} else if failure := microerrors.Parse(err.Error()); failure.Code != 400 || failure.Id != "ERROR_REASON_INVALID_ORDER_ID" {
			log.Fatalf("%s invalid order error = %v", encoding, failure)
		}
		fmt.Println(encoding, "found Order via etcd and received results")
	}
}

type OrderService struct{}

func (*OrderService) GetOrder(_ context.Context, request *OrderRequest, response *OrderResponse) error {
	if request.OrderID == "" {
		return microerrors.BadRequest("ERROR_REASON_INVALID_ORDER_ID", "order ID must not be blank")
	}
	response.OrderID = request.OrderID
	response.Status = "legacy-confirmed"
	return nil
}

func serveOrder(discovery registry.Registry, serviceName, readyFile string) (serveError error) {
	legacyServer := server.NewServer(server.Name(serviceName), server.Address("127.0.0.1:0"),
		server.Broker(memorybroker.NewBroker()), server.Registry(discovery), server.RegisterTTL(8*time.Second),
		server.RegisterInterval(2*time.Second))
	if err := legacyServer.Handle(legacyServer.NewHandler(&OrderService{})); err != nil {
		return err
	}
	if err := legacyServer.Start(); err != nil {
		return err
	}
	defer func() { serveError = errors.Join(serveError, legacyServer.Stop()) }()
	if err := os.WriteFile(readyFile+".tmp", []byte(legacyServer.Options().Address), 0600); err != nil {
		return err
	}
	if err := os.Rename(readyFile+".tmp", readyFile); err != nil {
		return err
	}
	_, err := io.Copy(io.Discard, os.Stdin)
	return err
}
