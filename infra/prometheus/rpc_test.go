package prometheus_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/prometheus"
)

func TestServiceMetricsRegisterKnownOperationsWithoutResettingSeries(t *testing.T) {
	metrics := prometheus.NewServiceRPCMetrics()
	const first = "/warehouse.v1.Inventory/Get"
	const second = "/warehouse.v1.Inventory/Count"
	if finish := metrics.BeginRPC(first, "grpc", "server"); finish != nil {
		t.Fatal("an unregistered method created an observation")
	}
	if err := metrics.RegisterRPCOperations(first); err != nil {
		t.Fatal(err)
	}
	finish := metrics.BeginRPC(first, "grpc", "server")
	if finish == nil {
		t.Fatal("the registered method did not create an observation")
	}
	finish(false, time.Millisecond)
	if err := metrics.RegisterRPCOperations(first, second); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, sample := range []string{
		`kronos_rpc_requests_total{operation="/warehouse.v1.Inventory/Get",outcome="success",protocol="grpc",side="server"} 1`,
		`kronos_rpc_in_flight{operation="/warehouse.v1.Inventory/Count",protocol="micro-v2",side="server"} 0`,
	} {
		if !strings.Contains(response.Body.String(), sample) {
			t.Errorf("missing sample %s", sample)
		}
	}
}
