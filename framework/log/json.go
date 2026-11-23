// Package log provides structured loggers with Kronos request correlation.
package log

import (
	"context"
	"io"
	"log/slog"

	"github.com/fangfengxiang/kronos/framework/request"
	kratoslog "github.com/go-kratos/kratos/v3/log"
)

// NewJSON returns a JSON logger that appends request_id when the log call
// context carries a Kronos request ID. Options configure the standard handler.
func NewJSON(output io.Writer, options *slog.HandlerOptions) *slog.Logger {
	return kratoslog.NewLogger(slog.NewJSONHandler(output, options), kratoslog.WithExtractor(requestAttributes))
}

func requestAttributes(ctx context.Context) []slog.Attr {
	if id := request.ID(ctx); id != "" {
		return []slog.Attr{slog.String("request_id", id)}
	}
	return nil
}
