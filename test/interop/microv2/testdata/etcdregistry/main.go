package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/micro/go-micro/v2/client"
	microerrors "github.com/micro/go-micro/v2/errors"
	"github.com/micro/go-micro/v2/registry"
	microetcd "github.com/micro/go-micro/v2/registry/etcd"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatal("usage: etcdregistry address service inspect|order")
	}
	discovery := microetcd.NewRegistry(registry.Addrs(os.Args[1]), registry.Timeout(3*time.Second))
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

type orderRequest struct {
	OrderID string `protobuf:"bytes,1,opt,name=order_id,json=orderId,proto3"`
}

func (message *orderRequest) Reset()         { *message = orderRequest{} }
func (message *orderRequest) String() string { return proto.CompactTextString(message) }
func (*orderRequest) ProtoMessage()          {}

type orderResponse struct {
	OrderID string `protobuf:"bytes,1,opt,name=order_id,json=orderId,proto3"`
	Status  string `protobuf:"bytes,2,opt,name=status,proto3"`
}

func (message *orderResponse) Reset()         { *message = orderResponse{} }
func (message *orderResponse) String() string { return proto.CompactTextString(message) }
func (*orderResponse) ProtoMessage()          {}

func callOrder(discovery registry.Registry, service string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rpcClient := client.NewClient(client.Registry(discovery), client.Retries(0))
	for _, encoding := range []string{"application/protobuf", "application/proto-rpc"} {
		request := func(id string) client.Request {
			return rpcClient.NewRequest(service, "OrderService.GetOrder", &orderRequest{OrderID: id}, client.WithContentType(encoding))
		}
		var response orderResponse
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
