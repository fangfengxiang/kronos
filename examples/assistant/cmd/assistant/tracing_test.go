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
	"github.com/fangfengxiang/kronos/framework/rpc"
	"go.opentelemetry.io/otel/propagation"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	otlp "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const (
	traceFixtureID       = "0123456789abcdef0123456789abcdef"
	traceFixtureParentID = "0123456789abcdef"
	traceFixtureState    = "vendor=kronos"
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

func sendTracedRequests(t *testing.T, ctx context.Context, httpEndpoint, grpcAddress, sseEndpoint, microEndpoint, requestID string) {
	t.Helper()
	parent := "00-" + traceFixtureID + "-" + traceFixtureParentID + "-01"
	sendTracedHTTP(t, ctx, httpEndpoint+"/v1/assistant:run", parent, requestID, `"text"`)
	sendTracedHTTP(t, ctx, sseEndpoint+"/v1/assistant:streamRun", parent, requestID, `"completed"`)

	parentContext := propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": parent, "tracestate": traceFixtureState})
	callContext := request.WithID(parentContext, requestID)
	grpc, err := assistant.NewRPCClient(ctx, grpcAddress, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer grpc.Close()
	if response, err := grpc.Run(callContext, &assistant.RunRequest{Prompt: "trace check"}); err != nil || response.GetText() == "" {
		t.Fatalf("traced gRPC call: %v, %v", response, err)
	}
	if err := grpc.StreamRun(callContext, &assistant.StreamRunRequest{Prompt: "trace check"}, func(*assistant.StreamRunResponse) error {
		return nil
	}); err != nil {
		t.Fatalf("traced gRPC stream: %v", err)
	}

	micro, err := assistant.NewRPCClient(ctx, microEndpoint, time.Second, rpc.WithProtocol(rpc.ProtocolMicroV2))
	if err != nil {
		t.Fatal(err)
	}
	defer micro.Close()
	if response, err := micro.Run(callContext, &assistant.RunRequest{Prompt: "trace check"}); err != nil || response.GetText() == "" {
		t.Fatalf("traced MicroRPC call: %v, %v", response, err)
	}
}

func sendTracedHTTP(t *testing.T, ctx context.Context, url, parent, requestID, expectedBody string) {
	t.Helper()
	traceRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{"prompt":"trace check"}`))
	if err != nil {
		t.Fatal(err)
	}
	traceRequest.Header.Set("Content-Type", "application/json")
	traceRequest.Header.Set("traceparent", parent)
	traceRequest.Header.Set("tracestate", traceFixtureState)
	traceRequest.Header.Set(request.IDHeader, requestID)
	response, err := http.DefaultClient.Do(traceRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), expectedBody) {
		t.Fatalf("traced %s returned HTTP %d, body %q, error %v", url, response.StatusCode, body, err)
	}
}

func (captured *capturedTraces) assertAssistantTraces(t *testing.T, processOutput []byte, requestID string) {
	t.Helper()
	captured.mu.Lock()
	defer captured.mu.Unlock()
	counts := make(map[string]int)
	linked := make(map[string]int)
	for _, span := range captured.spans {
		counts[span.GetName()]++
		if span.GetKind() != otlp.Span_SPAN_KIND_SERVER {
			t.Errorf("span %q kind = %v", span.GetName(), span.GetKind())
		}
		if hex.EncodeToString(span.GetTraceId()) == traceFixtureID && hex.EncodeToString(span.GetParentSpanId()) == traceFixtureParentID {
			linked[span.GetName()]++
			spanID := hex.EncodeToString(span.GetSpanId())
			if span.GetTraceState() != traceFixtureState {
				t.Errorf("exported %s span %s has tracestate %q, want %q", span.GetName(), spanID, span.GetTraceState(), traceFixtureState)
			}
			if !hasLogRecord(processOutput, func(record processLog) bool {
				return record.Event == "request_finished" && record.RequestID == requestID && record.TraceID == traceFixtureID && record.SpanID == spanID
			}) {
				t.Errorf("exported %s span %s could not be linked to the request log", span.GetName(), spanID)
			}
		}
	}
	if counts["Run"] != 16 || counts["StreamRun"] != 6 {
		t.Errorf("exported span counts = %v", counts)
	}
	if linked["Run"] != 3 || linked["StreamRun"] != 2 {
		t.Errorf("exported span counts for the W3C parent = %v, want 3 unary and 2 stream", linked)
	}
}
