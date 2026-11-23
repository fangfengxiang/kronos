package oteltrace_test

import (
	"bytes"
	"context"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/oteltrace"
	kratosTracing "github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3/transport"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	otlp "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestRequestTracingMatchesKratosParentPropagation(t *testing.T) {
	const parentHeader = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	headers := requestHeaders{make(http.Header)}
	headers.Set("traceparent", parentHeader)
	headers.Set("tracestate", "example=value")
	ctx := transport.NewServerContext(t.Context(), requestTransport{header: headers})

	baseline := tracetest.NewInMemoryExporter()
	baselineProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(baseline))
	t.Cleanup(func() { _ = baselineProvider.Shutdown(context.Background()) })
	upstream := kratosTracing.NewTracer(trace.SpanKindServer,
		kratosTracing.WithTracerProvider(baselineProvider), kratosTracing.WithPropagator(propagation.TraceContext{}))
	_, nativeSpan := upstream.Start(ctx, "Run", headers)
	nativeSpan.End()
	nativeSpans := baseline.GetSpans()
	if len(nativeSpans) != 1 {
		t.Fatalf("native Kratos produced %d spans", len(nativeSpans))
	}
	native := nativeSpans[0]
	traceID, parentID := native.SpanContext.TraceID(), native.Parent.SpanID()

	exports := make(chan *collector.ExportTraceServiceRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		data, err := io.ReadAll(request.Body)
		var payload collector.ExportTraceServiceRequest
		if err != nil || proto.Unmarshal(data, &payload) != nil || request.URL.Path != "/v1/traces" {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		exports <- &payload
		response.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(server.Close)
	provider, err := oteltrace.New(ctx, oteltrace.Config{ServiceName: "assistant", Endpoint: server.URL + "/v1/traces"})
	if err != nil {
		t.Fatal(err)
	}
	traceContext, finish := provider.StartRequest(ctx, "Run")
	fields := make(map[string]string)
	oteltrace.Propagate(traceContext, func(key, value string) { fields[key] = value })
	propagated := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(t.Context(), propagation.MapCarrier(fields)))
	if propagated.TraceID() != traceID || propagated.TraceState().String() != "example=value" {
		t.Errorf("propagated trace fields = %v", fields)
	}
	finish(true)
	flushContext, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := provider.Close(flushContext); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-exports:
		var spans []*otlp.Span
		for _, service := range payload.GetResourceSpans() {
			for _, scope := range service.GetScopeSpans() {
				spans = append(spans, scope.GetSpans()...)
			}
		}
		if len(spans) != 1 {
			t.Fatalf("expected one OTLP span, found %d", len(spans))
		}
		span := spans[0]
		if !bytes.Equal(span.GetTraceId(), traceID[:]) || !bytes.Equal(span.GetParentSpanId(), parentID[:]) ||
			span.GetKind() != otlp.Span_SPAN_KIND_SERVER || span.GetName() != native.Name ||
			span.GetStatus().GetCode() != otlp.Status_STATUS_CODE_ERROR || span.GetStatus().GetMessage() != "request failed" {
			t.Fatalf("unexpected exported span: %v", span)
		}
	case <-flushContext.Done():
		t.Fatal("OTLP endpoint did not receive a completed span")
	}
}

func TestNewRejectsInvalidTraceConfig(t *testing.T) {
	for _, config := range []oteltrace.Config{
		{Endpoint: "http://localhost:4318/v1/traces"},
		{ServiceName: "assistant", Endpoint: "grpc://localhost:4317"},
		{ServiceName: "assistant", Endpoint: "http://user:password@localhost:4318"},
	} {
		if _, err := oteltrace.New(t.Context(), config); err == nil {
			t.Error("invalid trace config was accepted")
		}
	}
}

type requestTransport struct {
	transport.Transporter
	header transport.Header
}

func (target requestTransport) RequestHeader() transport.Header { return target.header }

type requestHeaders struct{ http.Header }

func (header requestHeaders) Keys() []string { return slices.Collect(maps.Keys(header.Header)) }
