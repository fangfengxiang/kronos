//go:build etcd_interop && linux

package microv2_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func TestOrderCapacityBaseline(t *testing.T) {
	requestCount := os.Getenv("KRONOS_ORDER_LOAD_REQUESTS")
	if requestCount == "" {
		t.Skip("set KRONOS_ORDER_LOAD_REQUESTS to enable the Linux load test")
	}
	requests, err := strconv.Atoi(requestCount)
	if err != nil || requests < 64 || requests > 1_000_000 {
		t.Fatalf("KRONOS_ORDER_LOAD_REQUESTS = %q, want an integer from 64 to 1000000", requestCount)
	}

	registry := openTestRegistry(t)
	name := "orders.capacity." + rand.Text()
	waitForInstances(t, registry, name, kronos.ProtocolGRPC, 0)
	t.Logf("%s %s replicas=3 logical_cpus=%d gomaxprocs=%d",
		runtime.Version(), runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0))
	logOrderLoadResources(t, "before replicas")
	replicas := make([]*runningOrder, 3)
	for index := range replicas {
		replicas[index] = runOrderReplica(t, registry, name, order.NewService())
	}

	for _, protocol := range orderProtocols {
		t.Run(string(protocol), func(t *testing.T) {
			waitForInstances(t, registry, name, protocol, len(replicas))
			client, err := kronos.NewClient(kronos.Registry(registry), kronos.Name(name), kronos.Protocol(protocol))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := client.Close(); err != nil {
					t.Error(err)
				}
			})
			orders := order.NewOrderServiceClient(client)
			for range replicas {
				if got := getOrderStatus(t, orders); got != "confirmed" {
					t.Fatalf("warm-up order status = %q", got)
				}
			}
			for _, workers := range []int{1, 16, 64} {
				runOrderLoadPhase(t, orders, workers, requests)
			}
		})
	}

	for _, replica := range replicas {
		replica.stop()
	}
	for _, replica := range replicas {
		if err := replica.wait(t); err != nil {
			t.Fatal(err)
		}
	}
	for _, protocol := range orderProtocols {
		waitForInstances(t, registry, name, protocol, 0)
	}
	logOrderLoadResources(t, "after clients closed and replicas withdrew")
}

func runOrderLoadPhase(t *testing.T, client *order.OrderServiceClient, workers, requests int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	latencies := make([]time.Duration, requests)
	var wait sync.WaitGroup
	var recordFailure sync.Once
	var failure error
	start := time.Now()
	for worker := range workers {
		wait.Go(func() {
			for index := worker; index < requests && ctx.Err() == nil; index += workers {
				orderID := "订单-" + strconv.Itoa(index)
				started := time.Now()
				call, finish := context.WithTimeout(ctx, 3*time.Second)
				response, err := client.GetOrder(call, &order.GetOrderRequest{OrderId: orderID})
				finish()
				if err != nil || response.GetOrderId() != orderID || response.GetStatus() != "confirmed" {
					recordFailure.Do(func() {
						failure = fmt.Errorf("call %d returned %v, %v", index, response, err)
						cancel()
					})
					return
				}
				latencies[index] = time.Since(started)
			}
		})
	}
	wait.Wait()
	elapsed := time.Since(start)
	completed := slices.DeleteFunc(latencies, func(latency time.Duration) bool { return latency == 0 })
	if len(completed) != requests {
		t.Fatalf("workers=%d completed=%d/%d elapsed=%s call_error=%v context_error=%v",
			workers, len(completed), requests, elapsed, failure, ctx.Err())
	}
	slices.Sort(completed)
	t.Logf("workers=%d completed=%d elapsed=%s requests/s=%.0f p50=%s p95=%s p99=%s",
		workers, requests, elapsed.Round(time.Millisecond), float64(requests)/elapsed.Seconds(),
		completed[(requests*50-1)/100], completed[(requests*95-1)/100], completed[(requests*99-1)/100])
	logOrderLoadResources(t, "phase end")
}

func logOrderLoadResources(t *testing.T, stage string) {
	t.Helper()
	descriptors, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	memory, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(memory))
	if len(fields) < 2 {
		t.Fatalf("unexpected /proc/self/statm content %q", memory)
	}
	residentPages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: open_descriptors=%d goroutines=%d rss_bytes=%d", stage, len(descriptors), runtime.NumGoroutine(), residentPages*int64(os.Getpagesize()))
}
