// Package oteltrace adapts Kratos and OpenTelemetry tracing for Kronos services.
package oteltrace

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	kratosTracing "github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	kratoslog "github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/transport"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config identifies the service and its full OTLP HTTP trace endpoint.
type Config struct {
	ServiceName string
	Endpoint    string
}

// Provider owns an OTLP exporter and its request tracer.
type Provider struct {
	provider     *sdktrace.TracerProvider
	serverTracer *kratosTracing.Tracer
	clientTracer trace.Tracer
}

// New initializes an isolated provider. The owner closes it after all requests
// have finished so the exporter can flush completed spans.
func New(ctx context.Context, config Config) (*Provider, error) {
	if strings.TrimSpace(config.ServiceName) == "" {
		return nil, errors.New("trace service name is required")
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return nil, errors.New("trace endpoint requires an HTTP or HTTPS URL without credentials, query, or fragment")
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint.String()))
	if err != nil {
		return nil, fmt.Errorf("initialize OTLP HTTP trace exporter: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", config.ServiceName))),
	)
	serverTracer := kratosTracing.NewTracer(trace.SpanKindServer,
		kratosTracing.WithTracerProvider(provider), kratosTracing.WithPropagator(propagation.TraceContext{}),
		kratosTracing.WithTracerName("kronos"),
	)
	return &Provider{provider: provider, serverTracer: serverTracer, clientTracer: provider.Tracer("kronos")}, nil
}

// StartRequest starts a server span and returns a context and its finish
// function. Call finish once with the operation's failure status.
func (provider *Provider) StartRequest(ctx context.Context, operation string) (context.Context, func(failed bool)) {
	ctx, span := provider.startServer(ctx, operation)
	return traceLogContext(ctx), func(failed bool) { endSpan(span, failed) }
}

// StartRPC observes a unary client or server call. The completion callback
// receives the transport-neutral status and ends the span.
func (provider *Provider) StartRPC(ctx context.Context, operation, protocol, side string) (context.Context, func(status string)) {
	var span trace.Span
	if side == "client" {
		ctx, span = provider.clientTracer.Start(ctx, operation, trace.WithSpanKind(trace.SpanKindClient))
	} else {
		ctx, span = provider.startServer(ctx, operation)
	}
	span.SetAttributes(attribute.String("kronos.rpc.protocol", protocol), attribute.String("kronos.rpc.side", side))
	return traceLogContext(ctx), func(status string) {
		span.SetAttributes(attribute.String("kronos.rpc.status", status))
		endSpan(span, status != "OK")
	}
}

func (provider *Provider) startServer(ctx context.Context, operation string) (context.Context, trace.Span) {
	var carrier propagation.TextMapCarrier = propagation.MapCarrier(nil)
	if incoming, ok := transport.FromServerContext(ctx); ok {
		carrier = incoming.RequestHeader()
	}
	return provider.serverTracer.Start(ctx, operation, carrier)
}

func traceLogContext(ctx context.Context) context.Context {
	return kratoslog.ContextWithAttrs(ctx, kratosTracing.TraceAttrs(ctx)...)
}

func endSpan(span trace.Span, failed bool) {
	if failed {
		span.SetStatus(codes.Error, "request failed")
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

// Propagate sends the current span's W3C trace context through the given setter.
func (*Provider) Propagate(ctx context.Context, set func(string, string)) {
	Propagate(ctx, set)
}

// Close flushes completed traces and shuts down the exporter within ctx's budget.
func (provider *Provider) Close(ctx context.Context) error {
	if err := provider.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown trace exporter: %w", err)
	}
	return nil
}

// Propagate sends the current span's W3C trace context through the given setter.
func Propagate(ctx context.Context, set func(string, string)) {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return
	}
	fields := make(propagation.MapCarrier, 2)
	propagation.TraceContext{}.Inject(ctx, fields)
	for name, value := range fields {
		set(name, value)
	}
}
