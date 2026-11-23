package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fangfengxiang/kronos/framework/rpc"
)

type requestObservation struct {
	operation  string
	startedAt  time.Time
	finishSpan func(bool)
}

func (service admittingService) beginRequest(ctx context.Context, operation string) (context.Context, requestObservation) {
	observation := requestObservation{operation: operation, startedAt: time.Now()}
	if service.startTrace != nil {
		ctx, observation.finishSpan = service.startTrace(ctx, operation)
	}
	return ctx, observation
}

func (service admittingService) recordRequestResult(ctx context.Context, observation requestObservation, err error) {
	duration := time.Since(observation.startedAt)
	if observation.finishSpan != nil {
		observation.finishSpan(err != nil)
	}
	if service.recordMetrics != nil {
		service.recordMetrics(observation.operation, err != nil, duration)
	}
	level := slog.LevelInfo
	status, reason := "OK", ""
	if err != nil {
		level = slog.LevelWarn
		var failure *rpc.Error
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			status = "DEADLINE_EXCEEDED"
		case errors.Is(err, context.Canceled):
			status = "CANCELED"
		case errors.As(err, &failure) && failure != nil:
			status, reason = string(failure.Code), failure.Reason
		default:
			status = string(rpc.Internal)
		}
	}
	attributes := []slog.Attr{
		slog.String("event", "request_finished"),
		slog.String("operation", observation.operation),
		slog.String("status", status),
		slog.Int64("duration_us", duration.Microseconds()),
	}
	if reason != "" {
		attributes = append(attributes, slog.String("reason", reason))
	}
	service.logger.LogAttrs(ctx, level, "assistant request finished", attributes...)
}
