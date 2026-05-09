package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestNewLoggerFromContextHasTraceID(t *testing.T) {
	t.Parallel()
	traceID := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	spanID := trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled, Remote: true,
	}))
	id := TraceIDFromContext(ctx)
	if id == "" {
		t.Fatal("TraceIDFromContext empty")
	}
	if !strings.HasPrefix(id, "01020304") {
		t.Errorf("trace_id=%q, want prefix 01020304", id)
	}
}

func TestParseTraceparent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		header string
		want   string
	}{
		{"00-0123456789abcdef0123456789abcdef-0123456789abcdef-01", "0123456789abcdef0123456789abcdef"},
		{"", ""},
		{"00-bad", ""},
		{"01-0123456789abcdef0123456789abcdef-0123456789abcdef-00", ""}, // wrong version
		{"00-00000000000000000000000000000000-0123456789abcdef-01", ""}, // zero traceID
	}
	for _, tc := range tests {
		got := ParseTraceparent(tc.header)
		if got != tc.want {
			t.Errorf("ParseTraceparent(%q)=%q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestTraceIDFromRequestPrefersContext(t *testing.T) {
	t.Parallel()
	traceID := trace.TraceID{0xaa}
	spanID := trace.SpanID{0xbb}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	req.Header.Set("Traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	got := TraceIDFromRequest(req)
	if !strings.HasPrefix(got, "aa") {
		t.Errorf("got %q, want context-derived (prefix aa)", got)
	}
}

func TestTraceIDFromRequestFallsBackToHeader(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	got := TraceIDFromRequest(req)
	if got != "0123456789abcdef0123456789abcdef" {
		t.Errorf("got %q, want header-derived", got)
	}
}
