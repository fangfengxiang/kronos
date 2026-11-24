package prometheus

import (
	"net/http"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
)

// RPCMetrics records unified unary RPC traffic in its own Prometheus registry.
type RPCMetrics struct {
	series map[rpcLabels]rpcSeries
	scrape http.Handler
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
	counts, durations := requestVectors("operation", "protocol", "side", "outcome")
	inFlight := prom.NewGaugeVec(prom.GaugeOpts{
		Name: "kronos_rpc_in_flight",
		Help: "RPC calls currently being processed.",
	}, []string{"operation", "protocol", "side"})
	seriesByLabels := make(map[rpcLabels]rpcSeries, len(operations)*4)
	for _, operation := range operations {
		for _, protocol := range []string{"grpc", "micro-v2"} {
			for _, side := range []string{"client", "server"} {
				seriesByLabels[rpcLabels{operation: operation, protocol: protocol, side: side}] = rpcSeries{
					outcomes: requestOutcomes(counts, durations, operation, protocol, side),
					inFlight: inFlight.WithLabelValues(operation, protocol, side),
				}
			}
		}
	}
	return &RPCMetrics{series: seriesByLabels, scrape: scrapeHandler(counts, durations, inFlight)}, nil
}

// BeginRPC starts an observation. Call the returned function once on
// completion. An unregistered label combination returns nil.
func (metrics *RPCMetrics) BeginRPC(operation, protocol, side string) func(failed bool, duration time.Duration) {
	series, exists := metrics.series[rpcLabels{operation: operation, protocol: protocol, side: side}]
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
