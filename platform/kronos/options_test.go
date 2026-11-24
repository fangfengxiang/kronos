package kronos

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestDiagnosticsFollowsFinalObservabilityOptions(t *testing.T) {
	const address, alternateAddress = "127.0.0.1:8080", "127.0.0.1:8081"
	initial := &testScrapableMetrics{label: "initial"}
	replacement := &testScrapableMetrics{label: "replacement"}
	recordOnly := testRPCMetrics{}
	explicitHandler := http.NewServeMux()
	observe := Observability(ObservabilityConfig{DiagnosticsAddress: address, Metrics: initial})
	explicit := Diagnostics(alternateAddress, explicitHandler)

	for _, test := range []struct {
		name    string
		options []Option
		address string
		metrics RPCMetrics
		scrape  http.Handler
	}{
		{"combined default", []Option{observe}, address, initial, initial},
		{"metrics override", []Option{observe, Metrics(replacement)}, address, replacement, replacement},
		{"replace earlier metrics", []Option{Metrics(replacement), observe}, address, initial, initial},
		{"diagnostics-only composition", []Option{Metrics(replacement), Observability(ObservabilityConfig{DiagnosticsAddress: address})}, address, replacement, replacement},
		{"remove metrics", []Option{observe, Metrics(nil)}, address, nil, nil},
		{"record only", []Option{observe, Metrics(recordOnly)}, address, recordOnly, nil},
		{"explicit scrape", []Option{observe, explicit, Metrics(replacement)}, alternateAddress, replacement, explicitHandler},
		{"replace explicit scrape", []Option{explicit, observe, Metrics(replacement)}, address, replacement, replacement},
		{"explicit probes only", []Option{observe, Diagnostics(alternateAddress, nil)}, alternateAddress, initial, nil},
		{"disable diagnostics", []Option{observe, Diagnostics("", nil)}, "", initial, nil},
		{"metrics alone", []Option{Metrics(replacement)}, "", replacement, nil},
		{"empty observability preserves settings", []Option{observe, Observability(ObservabilityConfig{})}, address, initial, initial},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := applyOptions(test.options)
			if err != nil {
				t.Fatal(err)
			}
			if config.diagnostics != test.address || config.metrics != test.metrics || config.scrape != test.scrape {
				t.Fatalf("diagnostics = %q, metrics = %v, scrape = %v, want %q, %v, %v",
					config.diagnostics, config.metrics, config.scrape, test.address, test.metrics, test.scrape)
			}
		})
	}
}

func TestObservabilityPreservesIndependentTracingAndLogging(t *testing.T) {
	tracer := testRPCTracer("tracer")
	logger := slog.New(slog.DiscardHandler)
	configured := Observability(ObservabilityConfig{Tracer: tracer, Logger: logger})
	config, err := applyOptions([]Option{Tracing(testRPCTracer("earlier")), configured, Observability(ObservabilityConfig{})})
	if err != nil {
		t.Fatal(err)
	}
	if config.tracer != tracer || config.logger != logger {
		t.Fatalf("tracer = %v, logger = %p, want %v, %p", config.tracer, config.logger, tracer, logger)
	}
	config, err = applyOptions([]Option{configured, Tracing(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if config.tracer != nil || config.logger != logger {
		t.Fatalf("tracer = %v, logger = %p, want nil, %p", config.tracer, config.logger, logger)
	}
}

type testRPCMetrics struct{}

func (testRPCMetrics) BeginRPC(string, string, string) func(bool, time.Duration) { return nil }

type testScrapableMetrics struct {
	testRPCMetrics
	label string
}

func (*testScrapableMetrics) ServeHTTP(http.ResponseWriter, *http.Request) {}

type testRPCTracer string

func (testRPCTracer) StartRPC(ctx context.Context, _, _, _ string) (context.Context, func(string)) {
	return ctx, nil
}

func (testRPCTracer) Propagate(context.Context, func(string, string)) {}
