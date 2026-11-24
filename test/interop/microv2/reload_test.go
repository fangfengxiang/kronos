//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"context"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
)

func TestOrderReloadKeepsPublishedRPCAndHTTPListeners(t *testing.T) {
	registry := openTestRegistry(t)
	name := "orders.reload." + rand.Text()
	grpcAddress, microAddress, httpAddress := availableDiagnosticAddress(t), availableDiagnosticAddress(t), availableDiagnosticAddress(t)
	pidFile := filepath.Join(t.TempDir(), "order.pid")
	process := startOrderProcess(t, os.Getenv("KRONOS_ETCD_TEST_ADDRESS"), name,
		"KRONOS_ORDER_GRPC="+grpcAddress, "KRONOS_ORDER_MICRORPC="+microAddress,
		"KRONOS_ORDER_HTTP="+httpAddress, "KRONOS_ORDER_RELOAD_PID_FILE="+pidFile)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	originalPID := readOrderPID(t, ctx, pidFile, 0)
	if originalPID != process.command.Process.Pid {
		t.Fatalf("original Order PID %d, want %d", originalPID, process.command.Process.Pid)
	}
	if grpc, micro := waitForOrderAddresses(t, ctx, registry, name, process); grpc != grpcAddress || micro != microAddress {
		t.Fatalf("published RPC addresses = %q, %q", grpc, micro)
	}
	checkBusiness := func() {
		t.Helper()
		for _, protocol := range orderProtocols {
			client := newDiscoveredOrder(t, registry, name, protocol)
			response, err := client.GetOrder(ctx, &order.GetOrderRequest{OrderId: tracedOrderID})
			if err != nil || response.GetOrderId() != tracedOrderID || response.GetStatus() != "confirmed" {
				t.Fatalf("%s Order = %v, %v", protocol, response, err)
			}
		}
		assertOrderHTTP(t, ctx, httpAddress, "orders-reload")
	}
	checkBusiness()

	if err := process.command.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	replacementPID := readOrderPID(t, ctx, pidFile, originalPID)
	select {
	case <-process.done:
		if process.err != nil {
			t.Fatalf("original Order failed to drain: %v", process.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	checkBusiness()
	if err := syscall.Kill(replacementPID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	for _, protocol := range orderProtocols {
		waitForInstances(t, registry, name, protocol, 0)
	}
	check := time.NewTicker(20 * time.Millisecond)
	defer check.Stop()
	for _, address := range []string{grpcAddress, microAddress, httpAddress} {
		for {
			listener, err := new(net.ListenConfig).Listen(ctx, "tcp", address)
			if err == nil {
				listener.Close()
				break
			}
			select {
			case <-ctx.Done():
				t.Fatalf("wait for Order to release %s: %v", address, err)
			case <-check.C:
			}
		}
	}
}

func readOrderPID(t *testing.T, ctx context.Context, path string, previous int) int {
	t.Helper()
	check := time.NewTicker(20 * time.Millisecond)
	defer check.Stop()
	for {
		contents, readError := os.ReadFile(path)
		pid, parseError := strconv.Atoi(strings.TrimSpace(string(contents)))
		if readError == nil && parseError == nil && pid > 0 && pid != previous {
			return pid
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for Order PID after %d: %v", previous, ctx.Err())
		case <-check.C:
		}
	}
}
