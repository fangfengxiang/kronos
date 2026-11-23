package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/log"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
	kratoslogging "github.com/go-kratos/kratos/v3/middleware/logging"
)

func TestAssistantRequestLogsHaveSafeFields(t *testing.T) {
	const privatePrompt = "private-logging-test-prompt"
	const requestID = "origin_007"
	privateFailure := errors.New("private model error")
	input := &assistant.RunRequest{Prompt: privatePrompt}
	ctx := request.WithID(t.Context(), requestID)
	agent, err := newAgentService(failingTextRunner{err: privateFailure})
	if err != nil {
		t.Fatal(err)
	}
	var kronosOutput, kratosOutput bytes.Buffer
	kronosService := admittingService{ready: func() bool { return true }, next: agent, logger: log.NewJSON(&kronosOutput, nil)}
	if _, err := kronosService.Run(ctx, input); !errors.Is(err, privateFailure) {
		t.Fatalf("Assistant returned %v, want original model error at the private boundary", err)
	}
	upstreamLogger := slog.New(slog.NewJSONHandler(&kratosOutput, nil))
	upstream := kratoslogging.Server(upstreamLogger)(func(ctx context.Context, _ any) (any, error) {
		return agent.Run(ctx, input)
	})
	if _, err := upstream(ctx, input); !errors.Is(err, privateFailure) {
		t.Fatalf("Kratos returned %v, want original model error", err)
	}
	if !bytes.Contains(kratosOutput.Bytes(), []byte(privatePrompt)) || !bytes.Contains(kratosOutput.Bytes(), []byte(privateFailure.Error())) {
		t.Fatal("Kratos comparison input and error were not present in the default middleware output")
	}
	if bytes.Contains(kronosOutput.Bytes(), []byte(privatePrompt)) || bytes.Contains(kronosOutput.Bytes(), []byte(privateFailure.Error())) {
		t.Fatal("Assistant logged private request data or error text")
	}
	if !hasLogRecord(kronosOutput.Bytes(), func(record processLog) bool {
		return record.Event == "request_finished" && record.Operation == "Run" && record.RequestID == requestID && record.Status == string(rpc.Internal) && record.DurationMicros != nil
	}) {
		t.Fatalf("Assistant log is missing expected request fields: %s", &kronosOutput)
	}
}

type failingTextRunner struct{ err error }

func (runner failingTextRunner) Run(context.Context, string) (string, error) { return "", runner.err }

func (runner failingTextRunner) Stream(context.Context, string, func(string) error) error {
	return runner.err
}

type processLog struct {
	Event          string `json:"event"`
	Operation      string `json:"operation"`
	RequestID      string `json:"request_id"`
	TraceID        string `json:"trace_id"`
	SpanID         string `json:"span_id"`
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	Error          string `json:"error"`
	DurationMicros *int64 `json:"duration_us"`
}

func hasLogRecord(data []byte, matches func(processLog) bool) bool {
	for line := range bytes.Lines(data) {
		var record processLog
		if json.Unmarshal(line, &record) == nil && matches(record) {
			return true
		}
	}
	return false
}
