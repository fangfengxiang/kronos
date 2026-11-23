package binding

import (
	"context"
	"strings"

	"github.com/fangfengxiang/kronos/framework/localization"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
)

func withIncomingLanguage(ctx context.Context) context.Context {
	if serverTransport, ok := transport.FromServerContext(ctx); ok {
		values := serverTransport.RequestHeader().Values(localization.AcceptLanguageHeader)
		return localization.WithIncomingAcceptLanguage(ctx, values...)
	}
	return ctx
}

func propagateOutgoingLanguage(next middleware.Handler) middleware.Handler {
	return func(ctx context.Context, request any) (any, error) {
		values := localization.AcceptLanguage(ctx)
		if clientTransport, ok := transport.FromClientContext(ctx); ok && len(values) != 0 {
			clientTransport.RequestHeader().Set(localization.AcceptLanguageHeader, strings.Join(values, ","))
		}
		return next(ctx, request)
	}
}
