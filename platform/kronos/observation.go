package kronos

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fangfengxiang/kronos/framework/rpc"
)

type rpcObservation struct {
	service   string
	operation string
	protocol  rpc.Protocol
}

type rpcCompletion struct {
	started time.Time
	metric  func(failed bool, duration time.Duration)
	trace   func(status string)
}

func (observation rpcObservation) begin(ctx context.Context, config settings, side string) (context.Context, rpcCompletion) {
	completion := rpcCompletion{started: time.Now()}
	if config.metrics != nil {
		completion.metric = config.metrics.BeginRPC(observation.operation, string(observation.protocol), side)
	}
	if config.tracer != nil {
		ctx, completion.trace = config.tracer.StartRPC(ctx, observation.operation, string(observation.protocol), side)
	}
	return ctx, completion
}

func (completion rpcCompletion) finish(err error) time.Duration {
	duration := time.Since(completion.started)
	if completion.metric != nil {
		completion.metric(err != nil, duration)
	}
	if completion.trace != nil {
		status, _ := outcome(err)
		completion.trace(status)
	}
	return duration
}

func (observation rpcObservation) started(ctx context.Context, inFlight int64) {
	slog.DebugContext(ctx, "RPC request admitted", "event", "rpc_started", "service", observation.service,
		"operation", observation.operation, "protocol", observation.protocol, "in_flight", inFlight)
}

func (observation rpcObservation) finished(ctx context.Context, event string, duration time.Duration, err error, details ...slog.Attr) {
	level := slog.LevelDebug
	if err != nil {
		level = slog.LevelWarn
	}
	logger := slog.Default()
	if !logger.Enabled(ctx, level) {
		return
	}
	status, reason := outcome(err)
	fields := []slog.Attr{
		slog.String("event", event), slog.String("service", observation.service),
		slog.String("operation", observation.operation), slog.String("protocol", string(observation.protocol)),
		slog.String("status", status), slog.Bool("failed", err != nil),
		slog.Int64("duration_us", duration.Microseconds()),
	}
	if reason != "" {
		fields = append(fields, slog.String("reason", reason))
	}
	logger.LogAttrs(ctx, level, "RPC request completed", append(fields, details...)...)
}

func outcome(err error) (status, reason string) {
	switch {
	case err == nil:
		return "OK", ""
	case errors.Is(err, context.Canceled):
		return "CANCELED", ""
	case errors.Is(err, context.DeadlineExceeded):
		return "DEADLINE_EXCEEDED", ""
	case errors.Is(err, ErrClientClosed):
		return "CLIENT_CLOSED", ""
	}
	var failure *rpc.Error
	if errors.As(err, &failure) && failure != nil {
		switch failure.Code {
		case rpc.InvalidArgument, rpc.Unavailable, rpc.Internal:
			return string(failure.Code), failure.Reason
		}
	}
	return string(rpc.Internal), ""
}
