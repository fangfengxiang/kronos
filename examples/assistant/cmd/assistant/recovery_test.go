package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/log"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/oteltrace"
	"github.com/fangfengxiang/kronos/infra/prometheus"
	otlp "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestAssistantRecordsRecoveredRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	collectorEndpoint, captured := startTraceCollector(t)
	tracer, err := oteltrace.New(ctx, oteltrace.Config{ServiceName: "assistant", Endpoint: collectorEndpoint})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := tracer.Close(closeContext); err != nil {
			t.Error(err)
		}
	})
	metrics, err := prometheus.NewRequestMetrics("Run", "StreamRun")
	if err != nil {
		t.Fatal(err)
	}
	logFile, err := os.CreateTemp(t.TempDir(), "requests.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	service := admittingService{
		ready: func() bool { return true }, next: panickingAssistant{}, stream: panickingAssistant{},
		logger: log.NewJSON(logFile, nil), recordMetrics: metrics.Record, startTrace: tracer.StartRequest,
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := assistant.NewGRPCServerWithStream(listener, service, service)
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.Start(context.Background()) }()
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Stop(shutdown); err != nil {
			t.Error(err)
		}
		select {
		case err := <-serverResult:
			if err != nil {
				t.Error(err)
			}
		case <-shutdown.Done():
			t.Error("gRPC server did not stop")
		}
	})
	client, err := assistant.NewRPCClient(ctx, listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	const requestID = "recovered-request"
	ctx = request.WithID(ctx, requestID)
	unaryResponse, unaryError := client.Run(ctx, &assistant.RunRequest{})
	var fragments []string
	streamError := client.StreamRun(ctx, &assistant.StreamRunRequest{}, func(event *assistant.StreamRunResponse) error {
		fragments = append(fragments, event.GetTextDelta())
		return nil
	})
	for _, err := range []error{unaryError, streamError} {
		var failure *rpc.Error
		if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != assistant.ErrorReasonRunFailed.String() {
			t.Fatalf("recovered RPC error = %v", err)
		}
	}
	if unaryResponse != nil || len(fragments) != 1 || fragments[0] != "partial" {
		t.Fatalf("unexpected RPC output after recovery: %v, %v", unaryResponse, fragments)
	}
	if err := tracer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	logs, err := os.ReadFile(logFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(logs, []byte(`"event":"request_finished"`)) != 2 || bytes.Contains(logs, []byte(privateBusinessPanic)) {
		t.Errorf("expected two safe completion logs, got %s", logs)
	}
	scrape := httptest.NewRecorder()
	metrics.ServeHTTP(scrape, httptest.NewRequest("GET", "/metrics", nil))
	captured.mu.Lock()
	defer captured.mu.Unlock()
	if len(captured.spans) != 2 {
		t.Errorf("exported spans = %d, want 2", len(captured.spans))
	}
	for _, operation := range []string{"Run", "StreamRun"} {
		sample := `kronos_rpc_requests_total{operation="` + operation + `",outcome="error"} 1` + "\n"
		if !strings.Contains(scrape.Body.String(), sample) {
			t.Errorf("metrics are missing %q", sample)
		}
		found := false
		for _, span := range captured.spans {
			if span.GetName() != operation || span.GetStatus().GetCode() != otlp.Status_STATUS_CODE_ERROR {
				continue
			}
			found = hasLogRecord(logs, func(record processLog) bool {
				return record.Event == "request_finished" && record.Operation == operation && record.Status == string(rpc.Internal) &&
					record.RequestID == requestID && record.SpanID == hex.EncodeToString(span.GetSpanId())
			})
		}
		if !found {
			t.Errorf("%s has no failed span linked to its completion log", operation)
		}
	}
}

const privateBusinessPanic = "private panic from assistant business code"

type panickingAssistant struct{}

func (panickingAssistant) Run(context.Context, *assistant.RunRequest) (*assistant.RunResponse, error) {
	panic(privateBusinessPanic)
}

func (panickingAssistant) StreamRun(_ context.Context, _ *assistant.StreamRunRequest, emit func(string) error) error {
	if err := emit("partial"); err != nil {
		return err
	}
	panic(privateBusinessPanic)
}
