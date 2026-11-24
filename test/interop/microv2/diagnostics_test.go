//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

const orderOperation = "/" + order.OrderServiceName + "/GetOrder"

func availableDiagnosticAddress(t *testing.T) string {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func newDiagnosticClient(t *testing.T) *http.Client {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{}, Timeout: 2 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func diagnosticGET(t *testing.T, client *http.Client, address, path string, wantStatus int) string {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("diagnostic path %s returned %d, want %d", path, response.StatusCode, wantStatus)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func assertOrderInFlightMetric(t *testing.T, sample string, protocol kronos.WireProtocol, side string, want int) {
	t.Helper()
	labels := fmt.Sprintf(`operation=%q,protocol=%q,side=%q`, orderOperation, protocol, side)
	assertMetricLine(t, sample, fmt.Sprintf("kronos_rpc_in_flight{%s} %d", labels, want))
}

func assertOrderOutcomeMetrics(t *testing.T, sample string, protocol kronos.WireProtocol, side string, success, failure int) {
	t.Helper()
	for _, result := range []struct {
		outcome string
		count   int
	}{{"success", success}, {"error", failure}} {
		labels := fmt.Sprintf(`operation=%q,outcome=%q,protocol=%q,side=%q`, orderOperation, result.outcome, protocol, side)
		assertMetricLine(t, sample, fmt.Sprintf("kronos_rpc_requests_total{%s} %d", labels, result.count))
		assertMetricLine(t, sample, fmt.Sprintf("kronos_rpc_request_duration_seconds_count{%s} %d", labels, result.count))
	}
}

func assertMetricLine(t *testing.T, sample, line string) {
	t.Helper()
	if !strings.Contains(sample, "\n"+line+"\n") {
		t.Errorf("metrics missing %q", line)
	}
}
