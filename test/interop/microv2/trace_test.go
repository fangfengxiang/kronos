//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/infra/oteltrace"
	"github.com/fangfengxiang/kronos/infra/prometheus"
	"github.com/fangfengxiang/kronos/platform/kronos"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	otlp "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestOrderCorrelatesSignalsAcrossBothProtocols(t *testing.T) {
	clientLogger, clientLogPath := newRPCLogCapture(t)
	missingLogger, missingLogPath := newRPCLogCapture(t)
	registry := openTestRegistry(t)
	traceEndpoint, captured := orderTraceCollector(t)
	diagnostics := availableDiagnosticAddress(t)
	serviceName := "orders.trace." + rand.Text()
	process := startOrderProcess(t, os.Getenv("KRONOS_ETCD_TEST_ADDRESS"), serviceName,
		"KRONOS_ORDER_DIAGNOSTICS="+diagnostics, "KRONOS_ORDER_TRACE_ENDPOINT="+traceEndpoint)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	const requestID = "orders-trace-request"
	ctx = request.WithID(ctx, requestID)
	waitForOrderAddresses(t, ctx, registry, serviceName, process)

	tracer, err := oteltrace.New(ctx, oteltrace.Config{ServiceName: "order-test-client", Endpoint: traceEndpoint})
	if err != nil {
		t.Fatal(err)
	}
	flush := sync.OnceValue(func() error {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return tracer.Close(shutdown)
	})
	t.Cleanup(func() {
		if err := flush(); err != nil {
			t.Error(err)
		}
	})
	clientMetrics, err := prometheus.NewRPCMetrics(orderOperation)
	if err != nil {
		t.Fatal(err)
	}
	observability := kronos.ObservabilityConfig{Tracer: tracer, Metrics: clientMetrics, Logger: clientLogger}
	for _, protocol := range orderProtocols {
		orders := newDiscoveredOrder(t, registry, serviceName, protocol, kronos.Observability(observability))
		result, err := orders.GetOrder(ctx, &order.GetOrderRequest{OrderId: "trace-order"})
		if err != nil || result.GetStatus() != "confirmed" || result.GetOrderId() != "trace-order" {
			t.Fatalf("%s order = %v, %v", protocol, result, err)
		}
		_, err = orders.GetOrder(ctx, &order.GetOrderRequest{})
		var failure *kronos.Error
		if !errors.As(err, &failure) || failure.Code != kronos.InvalidArgument ||
			failure.Reason != "ERROR_REASON_INVALID_ORDER_ID" {
			t.Fatalf("%s invalid order = %v", protocol, err)
		}
	}
	observability.Logger = missingLogger
	missing := newDiscoveredOrder(t, registry, "orders.missing."+rand.Text(), kronos.ProtocolGRPC, kronos.Observability(observability))
	_, err = missing.GetOrder(ctx, &order.GetOrderRequest{OrderId: "not-discoverable"})
	var failure *kronos.Error
	if !errors.As(err, &failure) || failure.Code != kronos.Unavailable || failure.Reason != "NO_INSTANCES" {
		t.Fatalf("undiscoverable Order = %v", err)
	}

	serverSample := diagnosticGET(t, newDiagnosticClient(t), diagnostics, "/metrics", http.StatusOK)
	for _, protocol := range orderProtocols {
		assertOrderOutcomeMetrics(t, serverSample, protocol, "server", 1, 1)
	}
	clientScrape := httptest.NewRecorder()
	clientMetrics.ServeHTTP(clientScrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assertOrderOutcomeMetrics(t, clientScrape.Body.String(), kronos.ProtocolGRPC, "client", 1, 2)
	assertOrderOutcomeMetrics(t, clientScrape.Body.String(), kronos.ProtocolMicroV2, "client", 1, 1)

	// The reference command waits for both application drain and exporter flush.
	process.stop(t, 17*time.Second)
	if err := flush(); err != nil {
		t.Fatal(err)
	}
	clientLogs := loadRPCTraceLogs(t, clientLogPath)
	missingLogs := loadRPCTraceLogs(t, missingLogPath)
	if len(clientLogs) != 4 || len(missingLogs) != 1 {
		t.Fatalf("isolated client logs have %d and %d completed calls", len(clientLogs), len(missingLogs))
	}
	for _, entry := range missingLogs {
		if entry.Reason != "NO_INSTANCES" {
			t.Fatalf("unexpected event in missing service's log: %+v", entry)
		}
	}
	maps.Copy(clientLogs, missingLogs)
	serverLogs := loadRPCTraceLogs(t, process.logPath)
	for _, entries := range []map[string]rpcTraceLog{clientLogs, serverLogs} {
		for _, entry := range entries {
			if entry.RequestID != requestID {
				t.Errorf("log %s on %s carried request ID %q", entry.Event, entry.Protocol, entry.RequestID)
			}
		}
	}
	captured.assertSignals(t, clientLogs, serverLogs)
}

type recordedOrderTraces struct {
	mu    sync.Mutex
	spans []*otlp.Span
}

func orderTraceCollector(t *testing.T) (string, *recordedOrderTraces) {
	t.Helper()
	captured := new(recordedOrderTraces)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(response, request.Body, 1<<20))
		var batch collector.ExportTraceServiceRequest
		if err != nil || request.URL.Path != "/v1/traces" || proto.Unmarshal(data, &batch) != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		captured.mu.Lock()
		defer captured.mu.Unlock()
		for _, service := range batch.GetResourceSpans() {
			for _, scope := range service.GetScopeSpans() {
				captured.spans = append(captured.spans, scope.GetSpans()...)
			}
		}
		response.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(server.Close)
	return server.URL + "/v1/traces", captured
}

type rpcTraceLog struct {
	TraceID   string `json:"trace_id"`
	SpanID    string `json:"span_id"`
	RequestID string `json:"request_id"`
	Event     string
	Protocol  string
	Status    string
	Reason    string
	Attempts  *int
}

func loadRPCTraceLogs(t *testing.T, path string) map[string]rpcTraceLog {
	t.Helper()
	output, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	logs := make(map[string]rpcTraceLog)
	lines := bufio.NewScanner(bytes.NewReader(output))
	for lines.Scan() {
		var entry rpcTraceLog
		if err := json.Unmarshal(lines.Bytes(), &entry); err != nil {
			t.Fatalf("Order JSON log: %v", err)
		}
		if entry.Event == "rpc_finished" || entry.Event == "rpc_client_finished" {
			logs[entry.SpanID] = entry
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	return logs
}

func (captured *recordedOrderTraces) assertSignals(t *testing.T, clientLogs, serverLogs map[string]rpcTraceLog) {
	t.Helper()
	type callKey struct{ protocol, status string }
	type callSpans struct{ client, server *otlp.Span }
	calls := make(map[callKey]callSpans)
	captured.mu.Lock()
	defer captured.mu.Unlock()
	for _, span := range captured.spans {
		attributes := make(map[string]string, len(span.GetAttributes()))
		for _, attribute := range span.GetAttributes() {
			attributes[attribute.GetKey()] = attribute.GetValue().GetStringValue()
		}
		key := callKey{protocol: attributes["kronos.rpc.protocol"], status: attributes["kronos.rpc.status"]}
		wantCode := otlp.Status_STATUS_CODE_OK
		if key.status != "OK" {
			wantCode = otlp.Status_STATUS_CODE_ERROR
		}
		if span.GetName() != orderOperation || span.GetStatus().GetCode() != wantCode {
			t.Errorf("unexpected span name or status: %q, %v", span.GetName(), span.GetStatus())
		}
		pair := calls[key]
		switch span.GetKind() {
		case otlp.Span_SPAN_KIND_CLIENT:
			if pair.client != nil || attributes["kronos.rpc.side"] != "client" {
				t.Fatalf("unexpected client span for %+v", key)
			}
			pair.client = span
		case otlp.Span_SPAN_KIND_SERVER:
			if pair.server != nil || attributes["kronos.rpc.side"] != "server" {
				t.Fatalf("unexpected server span for %+v", key)
			}
			pair.server = span
		default:
			t.Fatalf("unexpected span kind %v", span.GetKind())
		}
		calls[key] = pair
	}
	if len(calls) != 5 || len(captured.spans) != 9 {
		t.Fatalf("exported %d calls and %d spans, want five calls and nine spans", len(calls), len(captured.spans))
	}
	lookup := calls[callKey{protocol: "grpc", status: "UNAVAILABLE"}]
	lookupLog := clientLogs[hex.EncodeToString(lookup.client.GetSpanId())]
	if lookup.client == nil || lookup.server != nil || lookupLog.Reason != "NO_INSTANCES" ||
		lookupLog.Attempts == nil || *lookupLog.Attempts != 0 || lookupLog.TraceID != hex.EncodeToString(lookup.client.GetTraceId()) {
		t.Errorf("undiscoverable call missing client-only evidence: %+v", lookupLog)
	}
	for _, protocol := range orderProtocols {
		for _, status := range []string{"OK", "INVALID_ARGUMENT"} {
			key := callKey{protocol: string(protocol), status: status}
			pair := calls[key]
			if pair.client == nil || pair.server == nil || !bytes.Equal(pair.client.GetTraceId(), pair.server.GetTraceId()) ||
				!bytes.Equal(pair.client.GetSpanId(), pair.server.GetParentSpanId()) {
				t.Errorf("%+v did not propagate the client span to the server", key)
				continue
			}
			traceID := hex.EncodeToString(pair.client.GetTraceId())
			clientEntry := clientLogs[hex.EncodeToString(pair.client.GetSpanId())]
			if clientEntry.TraceID != traceID || clientEntry.Protocol != key.protocol || clientEntry.Status != key.status {
				t.Errorf("%+v client span is missing correlated log fields", key)
			}
			if status != "OK" {
				serverEntry := serverLogs[hex.EncodeToString(pair.server.GetSpanId())]
				if serverEntry.TraceID != traceID || serverEntry.Protocol != key.protocol || serverEntry.Status != key.status ||
					serverEntry.Reason != "ERROR_REASON_INVALID_ORDER_ID" {
					t.Errorf("%+v server failure span is missing correlated log fields", key)
				}
			}
		}
	}
}
