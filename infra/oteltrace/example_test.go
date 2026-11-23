package oteltrace_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/fangfengxiang/kronos/infra/oteltrace"
)

func ExampleNew() {
	collector := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer collector.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	provider, err := oteltrace.New(ctx, oteltrace.Config{ServiceName: "assistant", Endpoint: collector.URL + "/v1/traces"})
	if err != nil {
		panic(err)
	}
	callContext, finish := provider.StartRequest(ctx, "Run")
	fields := make(map[string]string)
	oteltrace.Propagate(callContext, func(name, value string) { fields[name] = value })
	fmt.Println(fields["traceparent"] != "")
	finish(false)
	if err := provider.Close(ctx); err != nil {
		panic(err)
	}
	// Output: true
}
