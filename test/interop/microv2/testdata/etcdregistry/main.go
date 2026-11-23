package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/micro/go-micro/v2/registry"
	microetcd "github.com/micro/go-micro/v2/registry/etcd"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: etcdregistry address service")
	}
	discovery := microetcd.NewRegistry(registry.Addrs(os.Args[1]), registry.Timeout(3*time.Second))
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
