package consul_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/fangfengxiang/kronos/infra/consul"
)

func ExampleRegistry_Lookup() {
	agent := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `[{"Service":{"Address":"127.0.0.1","Port":9290}}]`)
	}))
	defer agent.Close()
	registry, err := consul.New(agent.URL)
	if err != nil {
		panic(err)
	}
	defer registry.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	addresses, err := registry.Lookup(ctx, "textbackend")
	if err != nil {
		panic(err)
	}
	fmt.Println(addresses)
	// Output: [127.0.0.1:9290]
}
