package binding

import (
	"context"
	"strings"

	"github.com/fangfengxiang/kronos/framework/localization"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/infra/oteltrace"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	"google.golang.org/grpc/metadata"
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
			setOutgoingRequest(ctx, clientTransport.RequestHeader().Set)
		}
		return next(ctx, payload)
	}
}

func withOutgoingStreamRequest(ctx context.Context) context.Context {
	// Kratos stream middleware runs after gRPC creates the stream.
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	if outgoing == nil {
		outgoing = make(metadata.MD)
	}
	setOutgoingRequest(ctx, func(key, value string) { outgoing.Set(key, value) })
	return metadata.NewOutgoingContext(ctx, outgoing)
}

func setOutgoingRequest(ctx context.Context, set func(string, string)) {
	if id := request.ID(ctx); id != "" {
		set(request.IDHeader, id)
	}
	if preferences := localization.AcceptLanguage(ctx); len(preferences) != 0 {
		set(localization.AcceptLanguageHeader, strings.Join(preferences, ","))
	}
	oteltrace.Propagate(ctx, set)
}
