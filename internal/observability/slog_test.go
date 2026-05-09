package observability

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestLoggerFromContextInjectsTraceFields(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	traceID := trace.TraceID{0xab, 0xcd}
	spanID := trace.SpanID{0x12, 0x34}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	ctx = ContextWithTenant(ctx, "tenant-x")
	logger := LoggerFromContext(ctx, base)
	logger.Info("hello")
	out := buf.String()
	if !strings.Contains(out, "trace_id") {
		t.Errorf("missing trace_id: %s", out)
	}
	if !strings.Contains(out, "tenant-x") {
		t.Errorf("missing tenant_id: %s", out)
	}
}

func TestLoggerFromContextNoTraceFallsBack(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	logger := LoggerFromContext(context.Background(), base)
	logger.Info("hello")
	if !strings.Contains(buf.String(), `"hello"`) {
		t.Errorf("expected hello message: %s", buf.String())
	}
}

func TestRequestLoggerMiddleware(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	mw := RequestLogger(base)
	captured := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l := LoggerFromRequest(r, base)
		l.Info("inside-handler")
		captured = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !captured {
		t.Fatal("handler not invoked")
	}
	if !strings.Contains(buf.String(), "0123456789abcdef0123456789abcdef") {
		t.Errorf("trace ID not propagated to log: %s", buf.String())
	}
}
