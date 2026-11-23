package binding

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/fangfengxiang/kronos/framework/localization"
	"github.com/fangfengxiang/kronos/framework/request"
	kratosmetadata "github.com/go-kratos/kratos/v3/middleware/metadata"
	"github.com/go-kratos/kratos/v3/transport"
)

func TestRequestMetadataUsesExactKeys(t *testing.T) {
	const suffixKey = "x-request-id-secondary"
	const id = "gateway_42"
	inbound := requestHeader{http.Header{
		"X-Request-Id":           {id},
		"X-Request-Id-Secondary": {"second"},
		"Authorization":          {"caller-owned"},
		"Accept-Language":        {"zh"},
	}}
	serverContext := transport.NewServerContext(t.Context(), requestTransport{header: inbound})
	noOpHandler := func(context.Context, any) (any, error) { return nil, nil }

	kronosHeaders := requestHeader{make(http.Header)}
	kronosContext := withIncomingRequest(serverContext)
	if got := request.ID(kronosContext); got != id {
		t.Fatalf("incoming Kronos request ID = %q, want %q", got, id)
	}
	kronosClient := transport.NewClientContext(kronosContext, requestTransport{header: kronosHeaders})
	if _, err := propagateOutgoingRequest(noOpHandler)(kronosClient, nil); err != nil {
		t.Fatal(err)
	}
	if kronosHeaders.Get(request.IDHeader) != id || len(kronosHeaders.Keys()) != 1 {
		t.Fatalf("Kronos propagated headers %v, want only the request ID", kronosHeaders.Header)
	}
	if _, err := propagateOutgoingRequest(noOpHandler)(localization.WithAcceptLanguage(kronosClient, "en"), nil); err != nil {
		t.Fatal(err)
	}
	if got := kronosHeaders.Get(localization.AcceptLanguageHeader); got != "en" {
		t.Fatalf("explicit outgoing language = %q, want en", got)
	}

	upstreamHeaders := requestHeader{make(http.Header)}
	propagatePrefix := kratosmetadata.WithPropagatedPrefix(request.IDHeader)
	upstreamClient := kratosmetadata.Client(propagatePrefix)(noOpHandler)
	upstreamServer := kratosmetadata.Server(propagatePrefix)(func(ctx context.Context, payload any) (any, error) {
		return upstreamClient(transport.NewClientContext(ctx, requestTransport{header: upstreamHeaders}), payload)
	})
	if _, err := upstreamServer(serverContext, nil); err != nil {
		t.Fatal(err)
	}
	if upstreamHeaders.Get(request.IDHeader) != id || upstreamHeaders.Get(suffixKey) != "second" || len(upstreamHeaders.Keys()) != 2 {
		t.Fatalf("Kratos with the same prefix propagated headers %v", upstreamHeaders.Header)
	}
}

type requestTransport struct {
	transport.Transporter
	header transport.Header
}

func (target requestTransport) RequestHeader() transport.Header { return target.header }

type requestHeader struct{ http.Header }

func (header requestHeader) Keys() []string { return slices.Collect(maps.Keys(header.Header)) }
