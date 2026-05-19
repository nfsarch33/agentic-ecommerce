//go:build v451_smoke

package v451_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ecotel "github.com/nfsarch33/helixon-ec/internal/observability/otel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestOTel_HTTPSpanCreated(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	handler := ecotel.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "expected exactly one span for HTTP request")

	span := spans[0]
	assert.Equal(t, "GET /api/v1/products", span.Name)
	assert.Contains(t, attrMap(span), "http.request.method")
	assert.Equal(t, "GET", attrMap(span)["http.request.method"])
}

func TestOTel_TraceContextPropagated(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	parentTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	parentSpanID := "00f067aa0ba902b7"

	var capturedTraceID string
	handler := ecotel.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc := oteltrace.SpanContextFromContext(r.Context())
		if sc.IsValid() {
			capturedTraceID = sc.TraceID().String()
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/product-publish", nil)
	req.Header.Set("Traceparent", "00-"+parentTraceID+"-"+parentSpanID+"-01")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, parentTraceID, capturedTraceID, "trace context should propagate from Traceparent header")
}

func TestOTel_ProviderShutdownDrainsPending(t *testing.T) {
	ctx := context.Background()
	provider, err := ecotel.InitProvider(ctx, ecotel.Config{
		ServiceName: "test-drain",
		Endpoint:    "localhost:4318",
		Insecure:    true,
	})
	require.NoError(t, err)

	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err = provider.Shutdown(shutdownCtx)
	assert.NoError(t, err, "shutdown should complete without error even with no collector")
}

func attrMap(span tracetest.SpanStub) map[string]string {
	result := make(map[string]string)
	for _, attr := range span.Attributes {
		result[string(attr.Key)] = attr.Value.Emit()
	}
	return result
}
