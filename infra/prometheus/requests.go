// Package prometheus adapts the Prometheus Go client for Kronos applications.
package prometheus

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
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
	if err := validateOperations(operations); err != nil {
		return nil, err
	}
	counts, durations := requestVectors("operation", "outcome")
	byOperation := make(map[string]operationMetrics, len(operations))
	for _, operation := range operations {
		byOperation[operation] = requestOutcomes(counts, durations, operation)
	}
	return &RequestMetrics{operations: byOperation, scrape: scrapeHandler(counts, durations)}, nil
}

func validateOperations(operations []string) error {
	if len(operations) == 0 {
		return errors.New("request metrics require at least one operation")
	}
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if strings.TrimSpace(operation) == "" {
			return errors.New("request metric operation is empty")
		}
		if _, exists := seen[operation]; exists {
			return fmt.Errorf("duplicate request metric operation %q", operation)
		}
		seen[operation] = struct{}{}
	}
	return nil
}

func requestVectors(labels ...string) (*prom.CounterVec, *prom.HistogramVec) {
	counts := prom.NewCounterVec(prom.CounterOpts{
		Name: "kronos_rpc_requests_total",
		Help: "Completed application requests.",
	}, labels)
	durations := prom.NewHistogramVec(prom.HistogramOpts{
		Name: "kronos_rpc_request_duration_seconds",
		Help: "Application request duration in seconds.",
	}, labels)
	return counts, durations
}

func requestOutcomes(counts *prom.CounterVec, durations *prom.HistogramVec, labels ...string) operationMetrics {
	successLabels := slices.Concat(labels, []string{"success"})
	errorLabels := slices.Concat(labels, []string{"error"})
	return operationMetrics{
		success: outcomeMetrics{
			count: counts.WithLabelValues(successLabels...), duration: durations.WithLabelValues(successLabels...),
		},
		failure: outcomeMetrics{
			count: counts.WithLabelValues(errorLabels...), duration: durations.WithLabelValues(errorLabels...),
		},
	}
}

func scrapeHandler(collectors ...prom.Collector) http.Handler {
	registry := prom.NewRegistry()
	registry.MustRegister(collectors...)
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// Record adds one observation. Unknown operations and negative durations are
// ignored.
func (metrics *RequestMetrics) Record(operation string, failed bool, duration time.Duration) {
	registered, exists := metrics.operations[operation]
	if !exists {
		return
	}
	registered.record(failed, duration)
}

func (metrics operationMetrics) record(failed bool, duration time.Duration) {
	if duration < 0 {
		return
	}
	outcome := metrics.success
	if failed {
		outcome = metrics.failure
	}
	outcome.count.Inc()
	outcome.duration.Observe(duration.Seconds())
}

// ServeHTTP exposes the registry using the Prometheus scrape protocol.
func (metrics *RequestMetrics) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	metrics.scrape.ServeHTTP(response, request)
}
