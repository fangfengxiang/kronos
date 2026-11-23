//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/infra/etcd"
	"github.com/fangfengxiang/kronos/platform/kronos"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestOrderThroughOfficialRegistryAndClients(t *testing.T) {
	endpoint := os.Getenv("KRONOS_ETCD_TEST_ADDRESS")
	if endpoint == "" {
		t.Fatal("set KRONOS_ETCD_TEST_ADDRESS to an isolated etcd host:port")
	}
	registry, err := etcd.New(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	serviceName := "go.micro.srv.order." + rand.Text()
	process := startOrderProcess(t, endpoint, serviceName)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	grpcAddress, microAddress := waitForOrderAddresses(t, ctx, registry, serviceName, process)
	cancel()
	if grpcAddress == microAddress {
		t.Fatalf("Order published the same address for both protocols: %s", grpcAddress)
	}
	assertOrderOverGRPC(t, grpcAddress)
	t.Run("Kronos clients to Order", func(t *testing.T) {
		assertTypedOrder(t, registry, serviceName, "confirmed")
		assertTypedOrder(t, registry, serviceName, "confirmed", kronos.Protocol(kronos.ProtocolMicroV2))
	})

	for _, version := range []string{"v2.0.0", "v2.9.1"} {
		t.Run(version, func(t *testing.T) {
			binary := buildUpstreamCommand(t, version, "./etcdregistry")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			output := runRegistryFixture(t, ctx, binary, endpoint, serviceName, "inspect")
			fields := strings.Fields(output)
			if len(fields) != 4 || fields[0] != serviceName || fields[1] != "1.0.0" || fields[3] != microAddress {
				t.Fatalf("official Registry nodes = %q, want only %s", output, microAddress)
			}
			t.Log(runRegistryFixture(t, ctx, binary, endpoint, serviceName, "order"))

			legacyName := "go.micro.srv.legacy-order." + rand.Text()
			legacyAddress := startFixtureProcess(t, ctx, binary, endpoint, legacyName, "serve")
			addresses, err := registry.Lookup(ctx, legacyName, "micro-v2")
			if err != nil || len(addresses) != 1 || addresses[0] != legacyAddress {
				t.Fatalf("official server publication = %v, %v, want %s", addresses, err, legacyAddress)
			}
			assertTypedOrder(t, registry, legacyName, "legacy-confirmed", kronos.Protocol(kronos.ProtocolMicroV2))
			defaultClient, err := kronos.NewClient(kronos.Registry(registry), kronos.Name(legacyName))
			if err != nil {
				t.Fatal(err)
			}
			defer defaultClient.Close()
			_, err = order.NewOrderServiceClient(defaultClient).GetOrder(ctx, &order.GetOrderRequest{OrderId: "订单-02"})
			var failure *kronos.Error
			if !errors.As(err, &failure) || failure.Code != kronos.Unavailable || failure.Reason != "NO_INSTANCES" {
				t.Fatalf("default gRPC against an old-protocol-only service = %v", err)
			}
		})
	}
	if err := process.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.done:
		if process.err != nil {
			t.Fatalf("Order stop: %v", process.err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("Order did not stop within its shutdown budget")
	}
	for _, protocol := range []string{"grpc", "micro-v2"} {
		addresses, err := registry.Lookup(t.Context(), serviceName, protocol)
		if err != nil || len(addresses) != 0 {
			t.Fatalf("%s after Order exit: %v, %v", protocol, addresses, err)
		}
	}
}

func assertTypedOrder(t *testing.T, registry *etcd.Registry, service, wantStatus string, options ...kronos.Option) {
	t.Helper()
	options = append([]kronos.Option{kronos.Registry(registry), kronos.Name(service)}, options...)
	client, err := kronos.NewClient(options...)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	orders := order.NewOrderServiceClient(client)
	result, err := orders.GetOrder(ctx, &order.GetOrderRequest{OrderId: "订单-02"})
	if err != nil || result.GetOrderId() != "订单-02" || result.GetStatus() != wantStatus {
		t.Fatalf("Kronos discovery call = %v, %v, want %q", result, err, wantStatus)
	}
	_, err = orders.GetOrder(ctx, &order.GetOrderRequest{})
	var failure *kronos.Error
	if !errors.As(err, &failure) || failure.Code != kronos.InvalidArgument || failure.Reason != "ERROR_REASON_INVALID_ORDER_ID" {
		t.Fatalf("Kronos blank order ID = %v", err)
	}
}

func runRegistryFixture(t *testing.T, ctx context.Context, binary, endpoint, service, command string) string {
	t.Helper()
	output, err := exec.CommandContext(ctx, binary, endpoint, service, command).CombinedOutput()
	if err != nil {
		t.Fatalf("official %s: %v\n%s", command, err, output)
	}
	return strings.TrimSpace(string(output))
}

type orderProcess struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func startOrderProcess(t *testing.T, endpoint, serviceName string) *orderProcess {
	t.Helper()
	repository, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "order")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	fixtureGo(t, ctx, repository, "build", "-o", binary, "./examples/order/cmd/order")
	process := &orderProcess{command: exec.Command(binary), done: make(chan struct{})}
	process.command.Env = append(os.Environ(),
		"KRONOS_ETCD_ENDPOINT="+endpoint, "KRONOS_ORDER_NAME="+serviceName,
		"KRONOS_ORDER_GRPC=127.0.0.1:0", "KRONOS_ORDER_MICRORPC=127.0.0.1:0", "KRONOS_ORDER_ADVERTISE_HOST=",
	)
	output, err := os.CreateTemp(t.TempDir(), "order-log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	process.command.Stdout, process.command.Stderr = output, output
	if err := process.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		process.err = process.command.Wait()
		close(process.done)
	}()
	t.Cleanup(func() {
		_ = process.command.Process.Kill()
		select {
		case <-process.done:
		case <-time.After(5 * time.Second):
			t.Error("Order test process did not exit")
		}
		if t.Failed() {
			logs, _ := os.ReadFile(output.Name())
			t.Logf("Order process output:\n%s", logs)
		}
	})
	return process
}

func waitForOrderAddresses(t *testing.T, ctx context.Context, registry *etcd.Registry, serviceName string, process *orderProcess) (string, string) {
	t.Helper()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		grpcAddresses, grpcErr := registry.Lookup(ctx, serviceName, "grpc")
		microAddresses, microErr := registry.Lookup(ctx, serviceName, "micro-v2")
		if grpcErr != nil || microErr != nil {
			t.Fatalf("discover Order: gRPC %v, MicroRPC %v", grpcErr, microErr)
		}
		if len(grpcAddresses) == 1 && len(microAddresses) == 1 {
			return grpcAddresses[0], microAddresses[0]
		}
		select {
		case <-process.done:
			t.Fatalf("Order exited before being discovered: %v", process.err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertOrderOverGRPC(t *testing.T, address string) {
	t.Helper()
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	const method = "/kronos.order.v1.OrderService/GetOrder"
	var result order.GetOrderResponse
	if err := connection.Invoke(ctx, method, &order.GetOrderRequest{OrderId: "订单-01"}, &result); err != nil ||
		result.GetOrderId() != "订单-01" || result.GetStatus() != "confirmed" {
		t.Fatalf("standard gRPC Order call = %v, %v", &result, err)
	}
	if err := connection.Invoke(ctx, method, &order.GetOrderRequest{}, &result); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("standard gRPC blank order ID = %v", err)
	}
}
