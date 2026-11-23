// Package prometheus adapts the Prometheus Go client for Kronos applications.
package prometheus

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// RequestMetrics records outcomes for a fixed set of operations. Its handler
// exposes metrics from this instance's registry.
type RequestMetrics struct {
	operations map[string]operationMetrics
	scrape     http.Handler
}

type operationMetrics struct {
	success outcomeMetrics
	failure outcomeMetrics
}

type outcomeMetrics struct {
	count    prom.Counter
	duration prom.Observer
}

// NewRequestMetrics prepares request counts and latency histograms for the
// supplied operation names. Each name must be non-empty and unique.
func NewRequestMetrics(operations ...string) (*RequestMetrics, error) {
	if len(operations) == 0 {
		return nil, errors.New("request metrics require at least one operation")
	}
	labels := []string{"operation", "outcome"}
	counts := prom.NewCounterVec(prom.CounterOpts{
		Name: "kronos_rpc_requests_total",
		Help: "Completed application requests.",
	}, labels)
	durations := prom.NewHistogramVec(prom.HistogramOpts{
		Name: "kronos_rpc_request_duration_seconds",
		Help: "Application request duration in seconds.",
	}, labels)
	byOperation := make(map[string]operationMetrics, len(operations))
	for _, operation := range operations {
		if strings.TrimSpace(operation) == "" {
			return nil, errors.New("request metric operation is empty")
		}
		if _, exists := byOperation[operation]; exists {
			return nil, fmt.Errorf("duplicate request metric operation %q", operation)
		}
		byOperation[operation] = operationMetrics{
			success: outcomeMetrics{
				count:    counts.WithLabelValues(operation, "success"),
				duration: durations.WithLabelValues(operation, "success"),
			},
			failure: outcomeMetrics{
				count:    counts.WithLabelValues(operation, "error"),
				duration: durations.WithLabelValues(operation, "error"),
			},
		}
	}
	registry := prom.NewRegistry()
	registry.MustRegister(counts, durations)
	return &RequestMetrics{operations: byOperation, scrape: promhttp.HandlerFor(registry, promhttp.HandlerOpts{})}, nil
}

// Record adds one observation. Unknown operations and negative durations are
// ignored.
func (metrics *RequestMetrics) Record(operation string, failed bool, duration time.Duration) {
	registered, exists := metrics.operations[operation]
	if !exists || duration < 0 {
		return
	}
	outcome := registered.success
	if failed {
		outcome = registered.failure
	}
	outcome.count.Inc()
	outcome.duration.Observe(duration.Seconds())
}

// ServeHTTP exposes the registry using the Prometheus scrape protocol.
func (metrics *RequestMetrics) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	metrics.scrape.ServeHTTP(response, request)
}
