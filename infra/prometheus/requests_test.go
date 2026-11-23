package prometheus_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/prometheus"
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestRequestMetricsMatchesDirectPrometheus(t *testing.T) {
	metrics, err := prometheus.NewRequestMetrics("Run")
	if err != nil {
		t.Fatal(err)
	}
	metrics.Record("Run", false, 1500*time.Millisecond)
	labels := prom.Labels{"operation": "Run", "outcome": "success"}
	registry := prom.NewRegistry()
	counter := prom.NewCounter(prom.CounterOpts{Name: "kronos_rpc_requests_total", ConstLabels: labels, Help: "Requests."})
	duration := prom.NewHistogram(prom.HistogramOpts{Name: "kronos_rpc_request_duration_seconds", ConstLabels: labels, Help: "Duration."})
	registry.MustRegister(counter, duration)
	counter.Inc()
	duration.Observe(1.5)
	upstreamOutput := scrape(t, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	kronosOutput := scrape(t, metrics)
	for _, sample := range []string{
		`kronos_rpc_requests_total{operation="Run",outcome="success"} 1`,
		`kronos_rpc_request_duration_seconds_count{operation="Run",outcome="success"} 1`,
		`kronos_rpc_request_duration_seconds_sum{operation="Run",outcome="success"} 1.5`,
	} {
		if !strings.Contains(upstreamOutput, sample+"\n") || !strings.Contains(kronosOutput, sample+"\n") {
			t.Errorf("expected sample %q in both direct and wrapped output", sample)
		}
	}
}

func TestRequestMetricsInstancesAndLabelsAreIsolated(t *testing.T) {
	first, err := prometheus.NewRequestMetrics("Run", "StreamRun")
	if err != nil {
		t.Fatal(err)
	}
	second, err := prometheus.NewRequestMetrics("Run")
	if err != nil {
		t.Fatal(err)
	}
	var calls sync.WaitGroup
	for range 20 {
		calls.Go(func() { first.Record("Run", true, time.Second) })
	}
	calls.Wait()
	first.Record("unknown operation", true, time.Second)
	first.Record("Run", false, -time.Second)
	firstOutput := scrape(t, first)
	secondOutput := scrape(t, second)
	if strings.Contains(firstOutput, "unknown operation") || !strings.Contains(firstOutput, `kronos_rpc_requests_total{operation="Run",outcome="success"} 0`+"\n") ||
		!strings.Contains(firstOutput, `kronos_rpc_requests_total{operation="Run",outcome="error"} 20`+"\n") ||
		!strings.Contains(secondOutput, `kronos_rpc_requests_total{operation="Run",outcome="error"} 0`+"\n") || strings.Contains(secondOutput, "StreamRun") {
		t.Fatal("the scrape cardinality or instance counters are incorrect")
	}
}

func TestRejectsInvalidOperationLists(t *testing.T) {
	for _, operations := range [][]string{nil, {" "}, {"Run", "Run"}} {
		if _, err := prometheus.NewRequestMetrics(operations...); err == nil {
			t.Errorf("invalid operations accepted: %q", operations)
		}
	}
}

func scrape(t *testing.T, handler http.Handler) string {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("scrape status %d: %s", response.Code, response.Body.String())
	}
	return response.Body.String()
}
