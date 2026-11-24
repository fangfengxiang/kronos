package prometheus

import (
	"net/http"
	"sync"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
)

// RPCMetrics records unified unary RPC traffic in its own Prometheus registry.
type RPCMetrics struct {
	mu            sync.RWMutex
	series        map[rpcLabels]rpcSeries
	scrape        http.Handler
	counts        *prom.CounterVec
	durations     *prom.HistogramVec
	inFlight      *prom.GaugeVec
	boundServices bool
}

type rpcLabels struct {
	operation string
	protocol  string
	side      string
}

type rpcSeries struct {
	outcomes operationMetrics
	inFlight prom.Gauge
}

// NewRPCMetrics registers the full Protobuf method paths used by these callers
// and servers. Only registered operations and supported protocols have series.
func NewRPCMetrics(operations ...string) (*RPCMetrics, error) {
	if err := validateOperations(operations); err != nil {
		return nil, err
	}
	metrics := newRPCMetrics(false)
	metrics.addOperations(operations)
	return metrics, nil
}

// NewServiceRPCMetrics creates an observer whose operation names come from
// the application's registered Protobuf services.
func NewServiceRPCMetrics() *RPCMetrics {
	return newRPCMetrics(true)
}

func newRPCMetrics(boundServices bool) *RPCMetrics {
	counts, durations := requestVectors("operation", "protocol", "side", "outcome")
	inFlight := prom.NewGaugeVec(prom.GaugeOpts{
		Name: "kronos_rpc_in_flight",
		Help: "RPC calls currently being processed.",
	}, []string{"operation", "protocol", "side"})
	return &RPCMetrics{
		series: make(map[rpcLabels]rpcSeries), scrape: scrapeHandler(counts, durations, inFlight),
		counts: counts, durations: durations, inFlight: inFlight, boundServices: boundServices,
	}
}

// RegisterRPCOperations prepares series for operations supplied by a service
// binding. Observers created with an explicit list retain their filter.
func (metrics *RPCMetrics) RegisterRPCOperations(operations ...string) error {
	if !metrics.boundServices {
		return nil
	}
	if err := validateOperations(operations); err != nil {
		return err
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.addOperations(operations)
	return nil
}

func (metrics *RPCMetrics) addOperations(operations []string) {
	for _, operation := range operations {
		for _, protocol := range []string{"grpc", "micro-v2"} {
			for _, side := range []string{"client", "server"} {
				labels := rpcLabels{operation: operation, protocol: protocol, side: side}
				if _, exists := metrics.series[labels]; exists {
					continue
				}
				metrics.series[labels] = rpcSeries{
					outcomes: requestOutcomes(metrics.counts, metrics.durations, operation, protocol, side),
					inFlight: metrics.inFlight.WithLabelValues(operation, protocol, side),
				}
			}
		}
	}
}

// BeginRPC starts an observation. Call the returned function once on
// completion. An unregistered label combination returns nil.
func (metrics *RPCMetrics) BeginRPC(operation, protocol, side string) func(failed bool, duration time.Duration) {
	metrics.mu.RLock()
	series, exists := metrics.series[rpcLabels{operation: operation, protocol: protocol, side: side}]
	metrics.mu.RUnlock()
	if !exists {
		return nil
	}
	series.inFlight.Inc()
	return func(failed bool, duration time.Duration) {
		series.inFlight.Dec()
		series.outcomes.record(failed, duration)
	}
}

// ServeHTTP exposes this instance's Prometheus metric families.
func (metrics *RPCMetrics) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	metrics.scrape.ServeHTTP(response, request)
}
