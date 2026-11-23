package main

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/request"
	"go.opentelemetry.io/otel/propagation"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	otlp "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const (
	traceFixtureID       = "0123456789abcdef0123456789abcdef"
	traceFixtureParentID = "0123456789abcdef"
)

type capturedTraces struct {
	mu    sync.Mutex
	spans []*otlp.Span
}

func startTraceCollector(t *testing.T) (string, *capturedTraces) {
	t.Helper()
	captured := &capturedTraces{}
	collectorServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		data, err := io.ReadAll(request.Body)
		var payload collector.ExportTraceServiceRequest
		if err != nil || proto.Unmarshal(data, &payload) != nil || request.URL.Path != "/v1/traces" {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		captured.mu.Lock()
		defer captured.mu.Unlock()
		for _, service := range payload.GetResourceSpans() {
			for _, scope := range service.GetScopeSpans() {
				captured.spans = append(captured.spans, scope.GetSpans()...)
			}
		}
		response.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(collectorServer.Close)
	return collectorServer.URL + "/v1/traces", captured
}

func sendTracedRequests(t *testing.T, ctx context.Context, httpEndpoint, grpcAddress, requestID string) {
	t.Helper()
	parent := "00-" + traceFixtureID + "-" + traceFixtureParentID + "-01"
	traceRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, httpEndpoint+"/v1/assistant:run", strings.NewReader(`{"prompt":"trace check"}`))
	if err != nil {
		t.Fatal(err)
	}
	traceRequest.Header.Set("Content-Type", "application/json")
	traceRequest.Header.Set("traceparent", parent)
	traceRequest.Header.Set(request.IDHeader, requestID)
	response, err := http.DefaultClient.Do(traceRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("traced request HTTP status = %d", response.StatusCode)
	}
	client, err := assistant.NewRPCClient(ctx, grpcAddress, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	parentContext := propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": parent})
	if err := client.StreamRun(request.WithID(parentContext, requestID), &assistant.StreamRunRequest{Prompt: "trace check"}, func(*assistant.StreamRunResponse) error {
		return nil
	}); err != nil {
		t.Fatalf("traced gRPC stream: %v", err)
	}
}

func (captured *capturedTraces) assertAssistantTraces(t *testing.T, processOutput []byte) {
	t.Helper()
	captured.mu.Lock()
	defer captured.mu.Unlock()
	counts := make(map[string]int)
	linkedSpanIDs := make(map[string]string)
	for _, span := range captured.spans {
		counts[span.GetName()]++
		if span.GetKind() != otlp.Span_SPAN_KIND_SERVER {
			t.Errorf("span %q kind = %v", span.GetName(), span.GetKind())
		}
		if hex.EncodeToString(span.GetTraceId()) == traceFixtureID && hex.EncodeToString(span.GetParentSpanId()) == traceFixtureParentID {
			linkedSpanIDs[span.GetName()] = hex.EncodeToString(span.GetSpanId())
		}
	}
	if counts["Run"] != 12 || counts["StreamRun"] != 5 {
		t.Errorf("exported span counts = %v", counts)
	}
	for _, operation := range []string{"Run", "StreamRun"} {
		spanID := linkedSpanIDs[operation]
		if spanID == "" || !hasLogRecord(processOutput, func(record processLog) bool {
			return record.Event == "request_finished" && record.TraceID == traceFixtureID && record.SpanID == spanID
		}) {
			t.Errorf("W3C parent context and exported %s span could not be linked to the request log", operation)
		}
	}
}
