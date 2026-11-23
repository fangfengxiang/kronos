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
	provider *sdktrace.TracerProvider
	tracer   *kratosTracing.Tracer
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
	tracer := kratosTracing.NewTracer(trace.SpanKindServer,
		kratosTracing.WithTracerProvider(provider), kratosTracing.WithPropagator(propagation.TraceContext{}),
		kratosTracing.WithTracerName("kronos"),
	)
	return &Provider{provider: provider, tracer: tracer}, nil
}

// StartRequest starts a server span and returns a context and its finish
// function. Call finish once with the operation's failure status.
func (provider *Provider) StartRequest(ctx context.Context, operation string) (context.Context, func(failed bool)) {
	var carrier propagation.TextMapCarrier = propagation.MapCarrier(nil)
	if incoming, ok := transport.FromServerContext(ctx); ok {
		carrier = incoming.RequestHeader()
	}
	ctx, span := provider.tracer.Start(ctx, operation, carrier)
	ctx = kratoslog.ContextWithAttrs(ctx, kratosTracing.TraceAttrs(ctx)...)
	return ctx, func(failed bool) {
		if failed {
			span.SetStatus(codes.Error, "request failed")
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
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
