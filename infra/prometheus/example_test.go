package prometheus_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/fangfengxiang/kronos/infra/prometheus"
)

func ExampleNewRequestMetrics() {
	metrics, err := prometheus.NewRequestMetrics("Run")
	if err != nil {
		panic(err)
	}
	metrics.Record("Run", false, 80*time.Millisecond)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for sample := range strings.SplitSeq(response.Body.String(), "\n") {
		if strings.HasPrefix(sample, `kronos_rpc_requests_total{operation="Run",outcome="success"}`) {
			fmt.Println(sample)
		}
	}
	// Output: kronos_rpc_requests_total{operation="Run",outcome="success"} 1
}
