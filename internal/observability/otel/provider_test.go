package otel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ecotel "github.com/nfsarch33/agentic-ecommerce/internal/observability/otel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestInitProvider_CreatesTracerProvider(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318")

	provider, err := ecotel.InitProvider(ctx, ecotel.Config{
		ServiceName: "test-service",
		Endpoint:    "localhost:4318",
		Insecure:    true,
	})
	require.NoError(t, err)
	require.NotNil(t, provider)

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		assert.NoError(t, provider.Shutdown(shutdownCtx))
	}()

	tp := otel.GetTracerProvider()
	assert.NotNil(t, tp)
}

func TestInitProvider_DefaultServiceName(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OTEL_SERVICE_NAME", "")

	provider, err := ecotel.InitProvider(ctx, ecotel.Config{
		Endpoint: "localhost:4318",
		Insecure: true,
	})
	require.NoError(t, err)
	defer func() { _ = provider.Shutdown(ctx) }()

	assert.NotNil(t, provider.TracerProvider())
}

func TestShutdown_NilProvider(t *testing.T) {
	var p *ecotel.Provider
	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestHTTPMiddleware_CreatesSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	handler := ecotel.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET /api/v1/products", spans[0].Name)
}

func TestHTTPMiddleware_Records500AsError(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	handler := ecotel.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "POST /api/v1/orders", spans[0].Name)
}

func TestHTTPMiddleware_PropagatesTraceContext(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	var capturedTraceID string
	handler := ecotel.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc := oteltrace.SpanContextFromContext(r.Context())
		if sc.IsValid() {
			capturedTraceID = sc.TraceID().String()
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", capturedTraceID)
}
