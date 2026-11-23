package binding

import (
	"context"
	"strings"

	"github.com/fangfengxiang/kronos/framework/localization"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
)

func withIncomingRequest(ctx context.Context) context.Context {
	serverTransport, ok := transport.FromServerContext(ctx)
	if !ok {
		return request.WithIncomingID(ctx)
	}
	header := serverTransport.RequestHeader()
	ctx = request.WithIncomingID(ctx, header.Values(request.IDHeader)...)
	return localization.WithIncomingAcceptLanguage(ctx, header.Values(localization.AcceptLanguageHeader)...)
}

func propagateOutgoingRequest(next middleware.Handler) middleware.Handler {
	return func(ctx context.Context, payload any) (any, error) {
		if clientTransport, ok := transport.FromClientContext(ctx); ok {
			header := clientTransport.RequestHeader()
			if id := request.ID(ctx); id != "" {
				header.Set(request.IDHeader, id)
			}
			if preferences := localization.AcceptLanguage(ctx); len(preferences) != 0 {
				header.Set(localization.AcceptLanguageHeader, strings.Join(preferences, ","))
			}
		}
		return next(ctx, payload)
	}
}
