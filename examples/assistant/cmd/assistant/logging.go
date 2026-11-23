package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fangfengxiang/kronos/framework/rpc"
)

func (service admittingService) recordRequestResult(ctx context.Context, operation string, startedAt time.Time, err error) {
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
		slog.String("operation", operation),
		slog.String("status", status),
		slog.Int64("duration_us", time.Since(startedAt).Microseconds()),
	}
	if reason != "" {
		attributes = append(attributes, slog.String("reason", reason))
	}
	service.logger.LogAttrs(ctx, level, "assistant request finished", attributes...)
}
