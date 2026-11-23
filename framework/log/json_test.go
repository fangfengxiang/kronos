package log_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/fangfengxiang/kronos/framework/log"
	"github.com/fangfengxiang/kronos/framework/request"
)

func TestLoggerInstancesAndRequestFields(t *testing.T) {
	var standardOutput, firstOutput, secondOutput bytes.Buffer
	standard := slog.New(slog.NewJSONHandler(&standardOutput, nil))
	first := log.NewJSON(&firstOutput, nil).With("service", "first")
	second := log.NewJSON(&secondOutput, &slog.HandlerOptions{Level: slog.LevelWarn}).With("service", "second")
	ctx := request.WithID(t.Context(), "gateway_42")
	standard.InfoContext(ctx, "request complete")
	first.InfoContext(ctx, "request complete")
	second.InfoContext(ctx, "filtered")
	if secondOutput.Len() != 0 {
		t.Fatalf("second logger did not filter INFO: %s", &secondOutput)
	}
	second.WarnContext(t.Context(), "startup failed")

	type fields struct {
		RequestID *string `json:"request_id"`
		Service   string  `json:"service"`
		Level     string  `json:"level"`
		Message   string  `json:"msg"`
	}
	var standardFields, firstFields, secondFields fields
	for _, result := range []struct {
		output *bytes.Buffer
		fields *fields
	}{
		{&standardOutput, &standardFields},
		{&firstOutput, &firstFields},
		{&secondOutput, &secondFields},
	} {
		if err := json.Unmarshal(result.output.Bytes(), result.fields); err != nil {
			t.Fatalf("decode JSON log: %v", err)
		}
	}
	if standardFields.RequestID != nil || standardFields.Message != firstFields.Message || standardFields.Level != firstFields.Level {
		t.Fatalf("unexpected comparison with standard slog JSON: standard=%+v Kronos=%+v", standardFields, firstFields)
	}
	if firstFields.RequestID == nil || *firstFields.RequestID != "gateway_42" || firstFields.Service != "first" {
		t.Fatalf("first logger produced %+v", firstFields)
	}
	if secondFields.RequestID != nil || secondFields.Service != "second" || secondFields.Level != "WARN" {
		t.Fatalf("second logger produced %+v", secondFields)
	}
}
