package log_test

import (
	"context"
	"log/slog"
	"os"

	"github.com/fangfengxiang/kronos/framework/log"
	"github.com/fangfengxiang/kronos/framework/request"
)

func ExampleNewJSON() {
	logger := log.NewJSON(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
			if attribute.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attribute
		},
	})
	ctx := request.WithID(context.Background(), "gateway_42")
	logger.InfoContext(ctx, "request complete")
	// Output: {"level":"INFO","msg":"request complete","request_id":"gateway_42"}
}
