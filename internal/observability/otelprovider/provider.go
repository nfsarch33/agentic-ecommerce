// Package otelprovider wires the MVP-1 trace path (v18870-2): one global
// TracerProvider per process with an OTLP/HTTP exporter, fail-open by
// construction. The exporter talks to a local trace backend bound to
// loopback; a down backend costs the batch processor a dropped span, never
// a request. Attributes ride an allow-list (this repository is PUBLIC: no
// hosts, prompts or PII leave the process in a span).
package otelprovider

import (
	"context"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// AllowedKeys is the complete span-attribute allow-list for this
// repository. Anything not on it is dropped before export. Keys are chosen
// so no value can carry a host, a prompt or PII: static methods, routes,
// status codes and opaque ids.
var AllowedKeys = map[string]struct{}{
	"temporal.activity.type": {},
	"workflow.name":          {},
	"http.method":            {},
	"http.route":             {},
	"http.status_code":       {},
	"run.id":                 {},
	"job.id":                 {},
	"agentrace.trace_id":     {},
}

// Setup installs a global TracerProvider exporting OTLP/HTTP to endpoint.
// An empty endpoint returns a no-op shutdown with the default provider
// left in place (tracing off). Loopback endpoints export insecurely; the
// backend is a local container by design (ADR-0107 C9).
func Setup(ctx context.Context, endpoint, serviceName string) (func(context.Context) error, error) {
	if strings.TrimSpace(endpoint) == "" {
		return func(context.Context) error { return nil }, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(u.Host)}
	if u.Scheme == "http" {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	// NewSchemaless: merging a second schema URL onto resource.Default()
	// conflicts (the SDK refuses mixed schemas); a schemaless attribute
	// set merges cleanly and keeps the default's schema.
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, err
	}
	// The allow-list processor wraps the batch processor: filtering happens
	// before anything is queued for export.
	batch := sdktrace.NewBatchSpanProcessor(exp)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(&AllowListProcessor{next: batch}),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = tp.ForceFlush(ctx) // best-effort: a down backend must not block shutdown
		return tp.Shutdown(ctx)
	}, nil
}

// Tracer returns the global tracer (noop unless Setup installed one).
func Tracer(name string) trace.Tracer { return otel.Tracer(name) }

// filterAttrs keeps only allow-listed keys.
func filterAttrs(attrs []attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		if _, ok := AllowedKeys[string(kv.Key)]; ok {
			out = append(out, kv)
		}
	}
	return out
}
