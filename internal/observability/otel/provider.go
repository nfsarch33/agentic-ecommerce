package otel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Provider wraps the OTel TracerProvider and exposes a single
// Shutdown method to drain pending spans on process exit.
type Provider struct {
	tp *sdktrace.TracerProvider
}

// Config controls OTel SDK initialization. Zero values fall back to
// env vars (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME,
// OTEL_RESOURCE_ATTRIBUTES).
type Config struct {
	ServiceName string
	Endpoint    string
	Insecure    bool
	Attributes  map[string]string
}

// InitProvider creates and registers a global TracerProvider with OTLP
// HTTP exporter. Call Shutdown on the returned Provider before process
// exit to flush buffered spans.
func InitProvider(ctx context.Context, cfg Config) (*Provider, error) {
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = os.Getenv("OTEL_SERVICE_NAME")
	}
	if serviceName == "" {
		serviceName = "agentic-ecommerce"
	}

	res, err := buildResource(ctx, serviceName, cfg.Attributes)
	if err != nil {
		return nil, fmt.Errorf("otel: build resource: %w", err)
	}

	exporter, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("otel: create exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Provider{tp: tp}, nil
}

// Shutdown flushes buffered spans and releases exporter resources.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.tp == nil {
		return nil
	}
	return p.tp.Shutdown(ctx)
}

// TracerProvider returns the underlying SDK TracerProvider for advanced
// use cases (e.g. registering custom span processors in tests).
func (p *Provider) TracerProvider() *sdktrace.TracerProvider {
	if p == nil {
		return nil
	}
	return p.tp
}

func buildResource(ctx context.Context, serviceName string, extra map[string]string) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
	}
	if envAttrs := os.Getenv("OTEL_RESOURCE_ATTRIBUTES"); envAttrs != "" {
		for _, pair := range strings.Split(envAttrs, ",") {
			k, v, ok := strings.Cut(pair, "=")
			if ok {
				attrs = append(attrs, attribute.String(strings.TrimSpace(k), strings.TrimSpace(v)))
			}
		}
	}
	for k, v := range extra {
		attrs = append(attrs, attribute.String(k, v))
	}
	return resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithTelemetrySDK(),
	)
}

func newExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}

	opts := []otlptracehttp.Option{}
	if endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
	}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	return otlptracehttp.New(ctx, opts...)
}
